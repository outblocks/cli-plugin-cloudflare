package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cloudflare/cloudflare-go"
	"github.com/gorilla/websocket"
	"github.com/outblocks/cli-plugin-cloudflare/cf"
	apiv1 "github.com/outblocks/outblocks-plugin-go/gen/api/v1"
	"github.com/outblocks/outblocks-plugin-go/util/errgroup"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type workerTailLog struct {
	Outcome    string `json:"outcome"`
	ScriptName string `json:"scriptName"`
	Exceptions []struct {
		Name      string `json:"name"`
		Message   string `json:"message"`
		Timestamp int    `json:"timestamp"`
	} `json:"exceptions"`
	Logs []struct {
		Message   []interface{} `json:"message"`
		Level     string        `json:"level"`
		Timestamp int           `json:"timestamp"`
	} `json:"logs"`
	EventTimestamp int64 `json:"eventTimestamp"`
	Event          struct {
		// RequestEvent.
		Request *struct {
			URL     string            `json:"url"`
			Method  string            `json:"method"`
			Headers map[string]string `json:"headers"`
			Cf      struct {
				ClientTCPRtt         int    `json:"clientTcpRtt"`
				Longitude            string `json:"longitude"`
				Latitude             string `json:"latitude"`
				TLSCipher            string `json:"tlsCipher"`
				Continent            string `json:"continent"`
				Asn                  int    `json:"asn"`
				ClientAcceptEncoding string `json:"clientAcceptEncoding"`
				Country              string `json:"country"`
				IsEUCountry          string `json:"isEUCountry"`
				TLSClientAuth        struct {
					CertIssuerDNLegacy    string `json:"certIssuerDNLegacy"`
					CertIssuerSKI         string `json:"certIssuerSKI"`
					CertSubjectDNRFC2253  string `json:"certSubjectDNRFC2253"`
					CertSubjectDNLegacy   string `json:"certSubjectDNLegacy"`
					CertFingerprintSHA256 string `json:"certFingerprintSHA256"`
					CertNotBefore         string `json:"certNotBefore"`
					CertSKI               string `json:"certSKI"`
					CertSerial            string `json:"certSerial"`
					CertIssuerDN          string `json:"certIssuerDN"`
					CertVerified          string `json:"certVerified"`
					CertNotAfter          string `json:"certNotAfter"`
					CertSubjectDN         string `json:"certSubjectDN"`
					CertPresented         string `json:"certPresented"`
					CertRevoked           string `json:"certRevoked"`
					CertIssuerSerial      string `json:"certIssuerSerial"`
					CertIssuerDNRFC2253   string `json:"certIssuerDNRFC2253"`
					CertFingerprintSHA1   string `json:"certFingerprintSHA1"`
				} `json:"tlsClientAuth"`
				TLSExportedAuthenticator struct {
					ClientFinished  string `json:"clientFinished"`
					ClientHandshake string `json:"clientHandshake"`
					ServerHandshake string `json:"serverHandshake"`
					ServerFinished  string `json:"serverFinished"`
				} `json:"tlsExportedAuthenticator"`
				TLSVersion                 string `json:"tlsVersion"`
				Colo                       string `json:"colo"`
				Timezone                   string `json:"timezone"`
				City                       string `json:"city"`
				EdgeRequestKeepAliveStatus int    `json:"edgeRequestKeepAliveStatus"`
				RequestPriority            string `json:"requestPriority"`
				HTTPProtocol               string `json:"httpProtocol"`
				Region                     string `json:"region"`
				RegionCode                 string `json:"regionCode"`
				AsOrganization             string `json:"asOrganization"`
				PostalCode                 string `json:"postalCode"`
			} `json:"cf"`
		} `json:"request"`
		Response *struct {
			Status int `json:"status"`
		} `json:"response"`

		// ScheduledEvent.
		Cron          string `json:"cron"`
		ScheduledTime string `json:"scheduledTime"`
	} `json:"event"`
}

func streamWorkerLogs(ctx context.Context, src string, t cloudflare.WorkersTail, filters []interface{}, srv apiv1.LogsPluginService_LogsServer) error {
	c, _, err := websocket.DefaultDialer.DialContext(ctx, t.URL, http.Header{
		"Sec-WebSocket-Protocol": []string{"trace-v1"},
		"User-Agent":             []string{"outblocks-cli"},
	})
	if err != nil {
		return err
	}

	defer c.Close()

	err = c.WriteJSON(map[string]interface{}{
		"filters": filters,
		"debug":   false,
	})
	if err != nil {
		return err
	}

	done := make(chan error)

	go func() {
		for {
			m := &workerTailLog{}

			_, message, err := c.ReadMessage()
			if err != nil {
				done <- err
				return
			}

			err = json.Unmarshal(message, &m)
			if err != nil {
				done <- err
				return
			}

			var (
				payload *apiv1.LogsResponse_Json
				h       *apiv1.LogsResponse_Http
			)

			if m.Event.Request != nil {
				h = &apiv1.LogsResponse_Http{
					RequestMethod: m.Event.Request.Method,
					RequestUrl:    m.Event.Request.URL,
					Status:        int32(m.Event.Response.Status),
					UserAgent:     m.Event.Request.Headers["user-agent"],
					Referer:       m.Event.Request.Headers["referer"],
					Latency:       nil,
					Protocol:      m.Event.Request.Cf.HTTPProtocol,
				}
			}

			eventPayload := make(map[string]interface{})

			if len(m.Exceptions) != 0 {
				eventPayload["exceptions"] = m.Exceptions
			}

			if len(m.Logs) != 0 {
				eventPayload["logs"] = m.Logs
			}

			if len(eventPayload) != 0 {
				p, err := structpb.NewStruct(eventPayload)
				if err != nil {
					done <- err
					return
				}

				payload = &apiv1.LogsResponse_Json{
					Json: p,
				}
			}

			err = srv.Send(&apiv1.LogsResponse{
				Source:   src,
				Severity: apiv1.LogSeverity_LOG_SEVERITY_INFO,
				Type:     apiv1.LogsResponse_TYPE_UNSPECIFIED,
				Time:     timestamppb.New(time.Unix(0, m.EventTimestamp*int64(time.Millisecond))),
				Http:     h,
				Payload:  payload,
			})
			if err != nil {
				done <- err
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err = <-done:
		return err
	}
}

func (p *Plugin) Logs(r *apiv1.LogsRequest, srv apiv1.LogsPluginService_LogsServer) error {
	ctx := srv.Context()
	pctx := p.PluginContext()

	if p.cli.AccountID == "" {
		return fmt.Errorf("$CLOUDFLARE_ACCOUNT_ID or secrets.cloudflare_account_id is required for logs")
	}

	if !r.Follow {
		p.log.Infoln("Cannot get old logs on Cloudflare, follow mode enabled by default...")
	}

	g, _ := errgroup.WithContext(ctx)

	var filters []interface{}

	if len(r.Contains) != 0 {
		filters = append(filters, map[string]string{
			"query": strings.Join(r.Contains, " "),
		})
	}

	for _, app := range r.Apps {
		if app.Type != AppTypeFunction {
			continue
		}

		scriptName := cf.ID(pctx.Env(), app.Id)
		rc := &cloudflare.ResourceContainer{
			Level:      cloudflare.AccountRouteLevel,
			Identifier: p.cli.AccountID,
		}
		app := app

		g.Go(func() error {
			t, err := p.cli.StartWorkersTail(ctx, rc, scriptName)
			if err != nil {
				return err
			}

			err = streamWorkerLogs(ctx, app.Id, t, filters, srv)
			_ = p.cli.DeleteWorkersTail(ctx, rc, scriptName, t.ID)

			return err
		})
	}

	return g.Wait()
}
