package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/bufbuild/connect-go"
	uploadv1 "github.com/morgangallant/buf-upload/gen/upload/v1"
	"github.com/morgangallant/buf-upload/gen/upload/v1/uploadv1connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

const (
	endpoint    = "https://upload-buf-testing-production.up.railway.app/"
	actAsClient = true
)

func main() {
	ctx, shutdown := signal.NotifyContext(context.Background(), os.Interrupt)
	if err := run(ctx, shutdown); err != nil {
		log.Fatalf("%+v", err)
	}
}

func run(ctx context.Context, shutdown func()) error {
	if actAsClient {
		client := uploadv1connect.NewUploadServiceClient(
			http.DefaultClient,
			endpoint,
		)
		buf := make([]byte, 200<<20) // 200 MB
		for i := range buf {
			if i%2 == 0 {
				buf[i] = 0x42
			} else {
				buf[i] = 0x69
			}
		}
		stream := client.Upload(ctx)
		const maxReqSize = 20 << 20 // 8 MB
		for i := 0; i < len(buf); i += maxReqSize {
			l := min(len(buf)-i, maxReqSize)
			if err := stream.Send(&uploadv1.UploadRequest{
				Name: "test",
				Data: buf[i : i+l],
			}); err != nil {
				return err
			}
			log.Printf("sent %d bytes", l)
		}
		resp, err := stream.CloseAndReceive()
		if err != nil {
			return err
		}
		log.Printf("received response: %+v", resp)
		return nil
	}

	mux := http.NewServeMux()
	path, handler := uploadv1connect.NewUploadServiceHandler(&uploadServer{})
	mux.Handle(path, handler)

	port, ok := os.LookupEnv("PORT")
	if !ok {
		port = "8080"
	}

	server := &http.Server{
		Addr:    ":" + port,
		Handler: h2c.NewHandler(mux, &http2.Server{}),
	}
	go func() {
		if err := server.ListenAndServe(); err != nil {
			log.Printf("server error: %+v", err)
			shutdown()
		}
	}()
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(sctx); err != nil {
			log.Printf("server shutdown error: %+v", err)
		}
	}()

	<-ctx.Done()
	return nil
}

type uploadServer struct {
	uploadv1connect.UnimplementedUploadServiceHandler
}

func (us *uploadServer) Upload(
	ctx context.Context,
	stream *connect.ClientStream[uploadv1.UploadRequest],
) (*connect.Response[uploadv1.UploadResponse], error) {
	resp := &uploadv1.UploadResponse{}
	for stream.Receive() {
		resp.Name = stream.Msg().GetName()
		resp.Size += int64(len(stream.Msg().GetData()))
	}
	if err := stream.Err(); err != nil {
		return nil, connect.NewError(connect.CodeUnknown, err)
	}
	return connect.NewResponse(resp), nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
