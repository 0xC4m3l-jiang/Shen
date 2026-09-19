// Command mirror 是接入形态①（旁路镜像）的接收端进程。
//
// 它监听一个 HTTP 端口，接收被镜像过来的请求副本，转成判定请求发给核心，
// 并把观测记为事件。它**不在业务请求路径上**，因此不处置、不改写、不回写。
package main

import (
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	judgev1 "shen/api/judge/v1"
	telemetryv1 "shen/api/telemetry/v1"
	"shen/edge/mirror"
)

const (
	defaultListen   = "127.0.0.1:8080"
	defaultCoreAddr = "127.0.0.1:9443"
	dialTimeout     = 5 * time.Second
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("mirror: %v", err)
	}
}

func run() error {
	listen := env("SHEN_MIRROR_LISTEN", defaultListen)
	coreAddr := env("SHEN_CORE_ADDR", defaultCoreAddr)

	// 与核心之间走本机明文 gRPC：TLS 在接入层（Envoy / Nginx）终结，
	// 且两者同主机。跨节点部署时必须换成 mTLS。
	conn, err := grpc.NewClient(coreAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	rcv := &mirror.Receiver{
		Judge:    judgev1.NewDeceptionJudgeClient(conn),
		Report:   telemetryv1.NewDeceptionTelemetryClient(conn),
		TrustXFF: true,
	}

	srv := &http.Server{
		Addr:              listen,
		Handler:           rcv,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("mirror 接收端已启动：监听 %s，核心 %s", listen, coreAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
