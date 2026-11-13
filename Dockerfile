FROM node:24-alpine AS assets
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
COPY internal/webui/templates/ /src/internal/webui/templates/
COPY internal/webui/timeline.go /src/internal/webui/timeline.go
COPY internal/webui/assets/theme.js internal/webui/assets/session.js /src/internal/webui/assets/
COPY licenses/ /src/licenses/
RUN npm run build
FROM golang:1.27.4-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY --from=assets /src/internal/webui/assets /src/internal/webui/assets
COPY scripts/ scripts/
COPY licenses/ licenses/
RUN CGO_ENABLED=0 go run scripts/notices.go > /go-notices.txt
RUN CGO_ENABLED=0 go build -trimpath -o /mailspace ./cmd/mailspace
FROM alpine:3.23 AS source
WORKDIR /source
COPY LICENSE README.md AGENTS.md CONTRIBUTING.md SECURITY.md CHANGELOG.md go.mod go.sum Makefile Dockerfile compose.yaml compose.personal.yaml .env.example .gitignore .dockerignore ./
COPY cmd/ cmd/
COPY internal/ internal/
COPY docs/ docs/
COPY licenses/ licenses/
COPY web/ web/
COPY .github/ .github/
COPY scripts/ scripts/
RUN tar -czf /source.tar.gz .
FROM alpine:3.23
RUN apk add --no-cache ca-certificates && addgroup -S mailspace && adduser -S -G mailspace mailspace
WORKDIR /app
RUN mkdir -p /app/data && chown mailspace:mailspace /app/data
COPY --from=backend /mailspace /app/mailspace
COPY licenses/ /app/licenses/
COPY --from=backend /go-notices.txt /app/licenses/THIRD_PARTY_GO.txt
COPY LICENSE /app/LICENSE
COPY --from=source /source.tar.gz /app/dist/source.tar.gz
USER mailspace
ENV MAILSPACE_ADDR=0.0.0.0:8080
EXPOSE 8080
ENTRYPOINT ["/app/mailspace"]
