package remote

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/knz/bubbline"
	"github.com/knz/bubbline/history"
	sqlv1 "github.com/litesql/go-ha/api/sql/v1"
	haconnect "github.com/litesql/go-ha/connect"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

var reSetDatabase = regexp.MustCompile(`(?i)^SET\s+DATABASE\s*(=|TO)\s*([^;\s]+)`)

var (
	white     = lipgloss.Color("255")
	gray      = lipgloss.Color("245")
	lightGray = lipgloss.Color("251")

	headerStyle  = lipgloss.NewStyle().Foreground(white).Bold(true).Align(lipgloss.Center)
	cellStyle    = lipgloss.NewStyle().Padding(0, 1)
	oddRowStyle  = cellStyle.Foreground(gray)
	evenRowStyle = cellStyle.Foreground(lightGray)
)

func Register(rootCmd *cobra.Command) error {
	cmd := cobra.Command{
		Use:   "remote URL",
		Short: "Connect to a remote pocketbase database via gRPC",
		Long: `Connect to a remote pocketbase database via gRPC.
The URL should be in the format of "http://host:port" or "https://host:port". 
Example:

Start the server with gRPC enabled:

	PB_GRPC_PORT=9090 pocketbase-ha serve

Then you can connect to the database using the CLI:

	pocketbase-ha remote http://localhost:9090

You can also specify an authentication token if the gRPC server requires authentication:
	
	pocketbase-ha remote http://localhost:9090 --token your_token_here

`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			remote := args[0]
			db, _ := cmd.Flags().GetString("db")
			token, _ := cmd.Flags().GetString("token")
			Start(remote, token, db)
		},
	}
	cmd.Flags().String("db", "data.db", "Default database")
	cmd.Flags().String("token", "", "Authentication token for gRPC server")
	rootCmd.AddCommand(&cmd)
	return nil
}

func Start(remote string, token string, replicationID string) {
	u, err := url.Parse(remote)
	if err != nil {
		slog.Error("parse url", "error", err)
		return
	}

	var dialOpts []grpc.DialOption

	if strings.HasPrefix(remote, "http://") {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{})))
	}
	if token != "" {
		dialOpts = append(dialOpts, grpc.WithPerRPCCredentials(grpcCredentials{token: token}))
	}

	cc, err := grpc.NewClient(u.Host, dialOpts...)
	if err != nil {
		slog.Error("grpc connect", "error", err)
		return
	}
	client := sqlv1.NewDatabaseServiceClient(cc)
	stream, err := client.Query(context.Background())
	if err != nil {
		slog.Error("stream", "error", err)
		return
	}
	defer stream.CloseSend()

	fmt.Println("Connected to", u.String(), "(Ctrl+D to exit)")

	reqChan := make(chan *sqlv1.QueryRequest)
	respChan := make(chan *sqlv1.QueryResponse)
	exitChan := make(chan struct{}, 1)

	m := bubbline.New()
	defer m.Close()

	go func() {
		for {
			select {
			case req := <-reqChan:
				err := stream.Send(req)
				if err != nil {
					m.Close()
					slog.Error(err.Error())
					exitChan <- struct{}{}
					return
				}
			case <-time.After(25 * time.Second):
				err := stream.Send(&sqlv1.QueryRequest{
					Type: sqlv1.QueryType_QUERY_TYPE_PING,
				})
				if err != nil {
					m.Close()
					slog.Error(err.Error())
					exitChan <- struct{}{}
					return
				}
				<-respChan // pong
			}
		}
	}()

	go func() {
		for {
			res, err := stream.Recv()
			if err != nil {
				m.Close()
				slog.Error(err.Error())
				exitChan <- struct{}{}
				return
			}
			respChan <- res
		}
	}()

	historyPath := filepath.Join(os.TempDir(), "pocketbase-ha_cli.history")
	h, _ := history.LoadHistory(historyPath)
	m.SetHistory(h)

	var command string
	for {
		select {
		case <-exitChan:
			return
		default:
			if command == "" {
				m.Prompt = fmt.Sprintf("%s> ", replicationID)
			} else {
				m.Prompt = ""
			}

			line, err := m.GetLine()
			if err != nil {
				if err == io.EOF {
					return
				}
				if errors.Is(err, bubbline.ErrInterrupted) {
					// Entered Ctrl+C to cancel input.
					fmt.Println("^C")
					command = ""
					continue
				} else if errors.Is(err, bubbline.ErrTerminated) {
					fmt.Println("terminated")
					return
				} else {
					fmt.Println("error:", err)
				}
				continue
			}
			command += line
			command = strings.TrimSpace(command)
			if !strings.HasSuffix(command, ";") {
				command += "\n"
				continue
			}

			m.AddHistoryEntry(command)
			history.SaveHistory(m.GetHistory(), historyPath)

			if strings.HasPrefix(strings.ToUpper(command), "EXIT") || strings.HasPrefix(strings.ToUpper(command), "QUIT") {
				return
			}

			if strings.HasPrefix(strings.ToUpper(command), "SHOW DATABASES") {
				command = ""
				resp, err := client.ReplicationIDs(context.Background(), &sqlv1.ReplicationIDsRequest{})
				if err != nil {
					slog.Error("databases", "error", err)
					continue
				}
				t := table.New().
					Border(lipgloss.NormalBorder()).
					BorderStyle(lipgloss.NewStyle().Foreground(white)).
					StyleFunc(func(row, col int) lipgloss.Style {
						switch {
						case row == table.HeaderRow:
							return headerStyle
						case row%2 == 0:
							return evenRowStyle
						default:
							return oddRowStyle
						}
					}).Headers("Databases")
				for _, row := range resp.ReplicationId {
					t.Row(row)
				}
				fmt.Println(t.Render())
				continue
			}

			if match := reSetDatabase.FindStringSubmatch(command); len(match) == 3 {
				replicationID = match[2]
				command = ""
				continue
			}

			reqChan <- &sqlv1.QueryRequest{
				Sql:           command,
				ReplicationId: replicationID,
			}
			command = ""
			resp := <-respChan
			if resp.Error != "" {
				fmt.Println(resp.Error)
				continue
			}
			if resp.ResultSet != nil {
				if len(resp.ResultSet.Columns) == 2 && resp.ResultSet.Columns[0] == "rows_affected" && len(resp.ResultSet.Rows) == 1 {
					fmt.Printf("%d rows affected\n", resp.RowsAffected)
					continue
				}
				t := table.New().
					Border(lipgloss.NormalBorder()).
					BorderStyle(lipgloss.NewStyle().Foreground(white)).
					StyleFunc(func(row, col int) lipgloss.Style {
						switch {
						case row == table.HeaderRow:
							return headerStyle
						case row%2 == 0:
							return evenRowStyle
						default:
							return oddRowStyle
						}
					}).Headers(resp.ResultSet.Columns...)
				for _, row := range resp.ResultSet.Rows {
					var cells []string
					for _, val := range row.Values {
						cells = append(cells, fmt.Sprint(haconnect.FromAnypb(val)))
					}
					t.Row(cells...)
				}
				fmt.Println(t.Render())
			}
			if resp.RowsAffected == 0 {
				continue
			}
			fmt.Printf("%d rows affected\n", resp.RowsAffected)
		}
	}
}

type grpcCredentials struct {
	token string
}

func (c grpcCredentials) GetRequestMetadata(ctx context.Context, in ...string) (map[string]string, error) {
	return map[string]string{
		"authorization": c.token,
	}, nil
}

func (c grpcCredentials) RequireTransportSecurity() bool {
	return false
}
