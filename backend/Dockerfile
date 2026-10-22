# ---------- 阶段 1：编译 ----------
# go.mod 里写的是 go 1.26.3 → 用 golang:1.26（本机也确认是 go1.26.3）
FROM golang:1.26 AS builder

WORKDIR /src

ENV GOPROXY=https://goproxy.cn,direct

# 先只拷依赖清单，让依赖层可以被 docker 缓存复用
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 → 产出静态二进制，才能塞进 alpine 这种精简基础镜像
# -trimpath / -s -w → 去掉本机路径与符号表，体积更小
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath -ldflags="-s -w" \
      -o /out/litstream ./cmd

# ---------- 阶段 2：运行 ----------
FROM alpine:3.20

# ca-certificates 必须装：你要出网调 DashScope 的 embedding / LLM 接口（HTTPS）
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -u 10001 app

WORKDIR /app

COPY --from=builder /out/litstream /app/litstream
# # .env 必须和 WORKDIR 同级，因为 viper.SetConfigFile(".env") 是按 CWD 找的
# COPY .env /app/.env
RUN mkdir -p /app/logs && chown -R app:app /app

USER app

EXPOSE 8080
CMD ["/app/litstream"]