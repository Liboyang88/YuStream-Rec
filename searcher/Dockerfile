# ---------- 阶段 1：编译 ----------
FROM golang:1.26 AS builder

WORKDIR /src

ENV GOPROXY=https://goproxy.cn,direct

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# 入口是当前模块根目录的 main 包（不是 ./cmd）
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath -ldflags="-s -w" \
      -o /out/searcher .

# ---------- 阶段 2：运行 ----------
FROM alpine:3.20

# searcher 要出网调 arXiv API + 阿里云百炼（LLM/翻译/embedding），必须带根证书和时区
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -u 10001 app

WORKDIR /app
COPY --from=builder /out/searcher /app/searcher

USER app
# 不要 EXPOSE，不要 CMD 常驻 —— 挂 CMD 只作为 -h 提示性的默认值
ENTRYPOINT ["/app/searcher"]
CMD ["-h"]