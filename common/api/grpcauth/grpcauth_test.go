package grpcauth

import (
	"context"
	"io"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func incoming(ctx context.Context, token string) context.Context {
	md := metadata.Pairs(MetadataKey, token)
	return metadata.NewIncomingContext(ctx, md)
}

// ── 空令牌 = 关闭：不装配任何拦截器 ──────────────────────────────────────────

func TestServerInterceptors_EmptyTokenDisabled(t *testing.T) {
	unary, stream := ServerInterceptors("  ")
	if unary != nil || stream != nil {
		t.Fatal("空令牌必须返回 nil 拦截器（未配置 = 关闭，回环部署行为不变）")
	}
}

// ── unary：缺头 / 错值 / 正确 ────────────────────────────────────────────────

func TestUnaryInterceptor(t *testing.T) {
	unary, stream := ServerInterceptors("tok-123")
	if unary == nil || stream == nil {
		t.Fatal("非空令牌必须返回成对拦截器")
	}
	handler := func(ctx context.Context, _ any) (any, error) { return "ok", nil }

	// 缺头：Unauthenticated（且不区分缺头与错值）
	_, err := unary(context.Background(), nil, nil, handler)
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("缺令牌：want Unauthenticated, got %v", err)
	}
	// 错值：Unauthenticated
	_, err = unary(incoming(context.Background(), "wrong"), nil, nil, handler)
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("错令牌：want Unauthenticated, got %v", err)
	}
	// 正确：放行
	out, err := unary(incoming(context.Background(), "tok-123"), nil, nil, handler)
	if err != nil || out != "ok" {
		t.Fatalf("正确令牌应放行：out=%v err=%v", out, err)
	}
	// 元数据键大小写不敏感（gRPC 规范）
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("X-Shen-Token", "tok-123"))
	if _, err = unary(ctx, nil, nil, handler); err != nil {
		t.Fatalf("大写键应等价放行：%v", err)
	}
}

// ── stream：校验通过后才进入 handler ────────────────────────────────────────

type fakeStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s fakeStream) Context() context.Context { return s.ctx }

func TestStreamInterceptor(t *testing.T) {
	_, stream := ServerInterceptors("tok-123")
	ran := false
	handler := func(_ any, ss grpc.ServerStream) error { ran = true; return nil }

	if err := stream(nil, fakeStream{ctx: context.Background()}, nil, handler); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("缺令牌：want Unauthenticated, got %v", err)
	}
	if ran {
		t.Fatal("校验失败时不得进入 handler")
	}
	if err := stream(nil, fakeStream{ctx: incoming(context.Background(), "tok-123")}, nil, handler); err != nil {
		t.Fatalf("正确令牌应放行：%v", err)
	}
	if !ran {
		t.Fatal("校验通过后应进入 handler")
	}
}

// ── 客户端凭证：随 RPC 发送令牌 ─────────────────────────────────────────────

func TestClientCredentials(t *testing.T) {
	var creds credentials.PerRPCCredentials = ClientCredentials("tok-123")
	md, err := creds.GetRequestMetadata(context.Background())
	if err != nil {
		t.Fatalf("GetRequestMetadata：%v", err)
	}
	if md[MetadataKey] != "tok-123" {
		t.Fatalf("元数据缺令牌：%v", md)
	}
	if creds.RequireTransportSecurity() {
		t.Fatal("明文回环连接也要能用（RequireTransportSecurity 必须为 false）")
	}
}

// ── 常数时间路径上不能被空令牌绕过 ──────────────────────────────────────────

func TestAuthorize_EmptyClientTokenRejected(t *testing.T) {
	unary, _ := ServerInterceptors("tok-123")
	// 客户端发空串令牌：metadata 会带上空值，必须拒绝。
	if _, err := unary(incoming(context.Background(), ""), nil, nil,
		func(context.Context, any) (any, error) { return nil, io.EOF }); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("空串令牌必须拒绝：%v", err)
	}
}
