FROM golang:1.27 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /app/easypost-mcp .

FROM alpine
COPY --from=public.ecr.aws/awsguru/aws-lambda-adapter:1.1.0 /lambda-adapter /opt/extensions/lambda-adapter
COPY --from=build /app/easypost-mcp /app/easypost-mcp
ENV AWS_LWA_READINESS_CHECK_PATH=/healthz
CMD ["/app/easypost-mcp", "-http", ":8080"]