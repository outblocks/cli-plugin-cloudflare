package cf

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"sort"

	"github.com/outblocks/cli-plugin-cloudflare/internal/config"
	"github.com/outblocks/outblocks-plugin-go/registry"
	"github.com/outblocks/outblocks-plugin-go/registry/fields"
	plugin_util "github.com/outblocks/outblocks-plugin-go/util"
	"github.com/outblocks/outblocks-plugin-go/util/errgroup"
	"github.com/zeebo/blake3"
)

const (
	CloudflarePagesMaxSize      = 25 * 1024 * 1024
	CloudflarePagesMaxFileCount = 20000
	UploadConcurrency           = 3
	UploadBucketMaxFiles        = 5000
	UploadBucketMaxSize         = 50 * 1024 * 1024
)

type PagesFiles struct {
	registry.ResourceBase

	Name        string `state:"-"`
	ProjectName fields.StringInputField
	Manifest    fields.MapOutputField

	Hashes     map[string]*PagesFileInfo `state:"-"`
	HashesList []string                  `state:"-"`

	missingHashes []string
}

func (o *PagesFiles) GetName() string {
	return o.Name
}

func (o *PagesFiles) CalculateDiff(ctx context.Context, meta interface{}) (registry.DiffType, error) {
	pctx := meta.(*config.PluginContext)
	pagesProject := o.ProjectName.Wanted()

	if pagesProject != "" {
		cli := pctx.WranglerCloudflareClient().PagesAPI(pagesProject)

		var err error

		o.missingHashes, err = cli.MissingHashes(ctx, o.HashesList)
		if err != nil {
			return registry.DiffTypeNone, fmt.Errorf("error checking cloudflare pages missing files: %w", err)
		}
	} else {
		o.missingHashes = o.HashesList
	}

	manifest := make(map[string]string)

	for _, h := range o.Hashes {
		manifest[fmt.Sprintf("/%s", h.Rel)] = h.Hash
	}

	manifestInterface := make(map[string]interface{}, len(manifest))
	for k, v := range manifest {
		manifestInterface[k] = v
	}

	o.Manifest.SetCurrent(manifestInterface)

	if len(o.missingHashes) == 0 {
		return registry.DiffTypeNone, nil
	}

	return registry.DiffTypeProcess, nil
}

func (o *PagesFiles) Process(ctx context.Context, meta interface{}) error {
	pctx := meta.(*config.PluginContext)
	pagesProject := o.ProjectName.Wanted()
	cli := pctx.WranglerCloudflareClient().PagesAPI(pagesProject)

	uploadFiles := make([]*PagesFileInfo, len(o.missingHashes))
	for i, h := range o.missingHashes {
		uploadFiles[i] = o.Hashes[h]
	}

	err := pagesUpload(ctx, cli, uploadFiles)
	if err != nil {
		return fmt.Errorf("error uploading cloudflare pages files: %w", err)
	}

	err = cli.UpsertHashes(ctx, o.HashesList)
	if err != nil {
		return fmt.Errorf("error upserting cloudflare pages file hashes: %w", err)
	}

	return nil
}

type uploadBucket struct {
	files         []*PagesFileInfo
	remainingSize int64
}

func (b *uploadBucket) payload() ([]map[string]interface{}, error) {
	ret := make([]map[string]interface{}, len(b.files))

	for i, f := range b.files {
		bytes, err := os.ReadFile(f.Path)
		if err != nil {
			return nil, err
		}

		ret[i] = map[string]interface{}{
			"key":   f.Hash,
			"value": base64.StdEncoding.EncodeToString(bytes),
			"metadata": map[string]string{
				"contentType": f.ContentType,
			},
			"base64": true,
		}
	}

	return ret, nil
}

func pagesUpload(ctx context.Context, cli *config.WranglerCloudflarePagesAPI, files []*PagesFileInfo) error {
	sort.Slice(files, func(i int, j int) bool {
		return files[i].Size < files[j].Size
	})

	bucketOffset := 0
	buckets := make([]*uploadBucket, UploadConcurrency)

	for i := 0; i < UploadConcurrency; i++ {
		buckets[i] = &uploadBucket{
			remainingSize: UploadBucketMaxSize,
		}
	}

	for _, f := range files {
		inserted := false

		for i := range buckets {
			b := buckets[(bucketOffset+i)%len(buckets)]

			if b.remainingSize < f.Size {
				continue
			}

			if len(b.files) >= UploadBucketMaxFiles {
				continue
			}

			b.files = append(b.files, f)
			b.remainingSize -= f.Size
			inserted = true

			break
		}

		if !inserted {
			newBucket := &uploadBucket{
				files:         []*PagesFileInfo{f},
				remainingSize: UploadBucketMaxSize - f.Size,
			}

			buckets = append(buckets, newBucket)
		}

		bucketOffset++
	}

	g, _ := errgroup.WithConcurrency(ctx, UploadConcurrency)

	for _, b := range buckets {
		b := b

		if len(b.files) == 0 {
			continue
		}

		g.Go(func() error {
			payload, err := b.payload()
			if err != nil {
				return err
			}

			return cli.UploadBucket(ctx, payload)
		})
	}

	return g.Wait()
}

type PagesFileInfo struct {
	Path        string
	Rel         string
	Hash        string
	Size        int64
	ContentType string
}

func PagesHashFile(f string) (string, error) {
	bytes, err := os.ReadFile(f)
	if err != nil {
		return "", err
	}

	ext := filepath.Ext(f)
	if ext != "" {
		ext = ext[1:]
	}

	data := base64.StdEncoding.EncodeToString(bytes)
	sum := blake3.Sum256([]byte(data + ext))

	return hex.EncodeToString(sum[:])[:32], nil
}

func PagesFindFiles(root string, patterns []string) (ret map[string]*PagesFileInfo, err error) {
	ret = make(map[string]*PagesFileInfo)

	err = plugin_util.WalkWithExclusions(root, patterns, func(path, rel string, info os.FileInfo) error {
		if info.IsDir() {
			return nil
		}

		if info.Size() > CloudflarePagesMaxSize {
			return fmt.Errorf("CloudFlare Pages only supports files up to %d bytes in size\nfile: %s size: %d", CloudflarePagesMaxFileCount, path, info.Size())
		}

		hash, err := PagesHashFile(path)
		if err != nil {
			return err
		}

		contentType := mime.TypeByExtension(filepath.Ext(path))
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		ret[hash] = &PagesFileInfo{
			Hash:        hash,
			Path:        path,
			Rel:         rel,
			Size:        info.Size(),
			ContentType: contentType,
		}

		return nil
	})

	return ret, err
}
