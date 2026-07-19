# 多阶段构建：第一阶段编译，第二阶段用精简镜像运行。
FROM golang:1.26-alpine AS build
WORKDIR /src

# 先拷贝依赖清单以利用缓存
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
# 静态编译，产物无需 libc
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/ivideo-server .

FROM alpine:3.20
WORKDIR /app
RUN apk add --no-cache ca-certificates
COPY --from=build /out/ivideo-server .
EXPOSE 3001
ENTRYPOINT ["/app/ivideo-server"]
