# ==========================================
# Giai đoạn 1: Biên dịch mã nguồn (Builder)
# ==========================================
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Cài đặt các công cụ biên dịch thiết yếu
RUN apk add --no-cache git build-base

# Copy go.mod và go.sum trước để tận dụng Docker cache layer
COPY go.mod ./
# Ở Phase 0, chưa có go.sum do chưa cài các dependencies ngoài qua CLI.

# Copy toàn bộ mã nguồn backend
COPY . .

# Biên dịch tĩnh hai chương trình REST API và WS Gateway
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/gateway ./cmd/gateway

# ==========================================
# Giai đoạn 2: Tạo ảnh thực thi (Runtime)
# ==========================================
FROM alpine:3.19

WORKDIR /app

# Cài đặt chứng chỉ CA cho các yêu cầu HTTPS an toàn và múi giờ quốc tế
RUN apk --no-cache add ca-certificates tzdata

# Copy các file nhị phân đã được biên dịch từ giai đoạn builder
COPY --from=builder /app/api /app/api
COPY --from=builder /app/gateway /app/gateway

# Khai báo cổng lắng nghe mặc định: 8080 (REST API) và 8081 (WS Gateway)
EXPOSE 8080
EXPOSE 8081

# Mặc định khởi chạy REST API Server. Khi chạy Gateway có thể ghi đè lệnh CMD.
ENTRYPOINT ["/app/api"]
