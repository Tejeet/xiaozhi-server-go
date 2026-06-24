FROM alpine:latest

# 运行时依赖：ca-certificates 用于对外 HTTPS（LLM/TTS 等）。
# 二进制为静态链接 + UPX 压缩，无需 glibc/sqlite 动态库。
RUN apk add --no-cache ca-certificates

WORKDIR /app

# 应用二进制（静态链接，linux/amd64）+ 运行时资源
COPY linux-amd64-server-upx /app/linux-amd64-server-upx
COPY config.yaml /app/config.yaml
COPY web /app/web
COPY music /app/music

# 提交到 git 的二进制缺少可执行位，这里补上
RUN chmod +x /app/linux-amd64-server-upx

EXPOSE 8000 8080

CMD ["./linux-amd64-server-upx"]
