# syntax=docker/dockerfile:1

FROM node:24-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

FROM golang:1.27-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/frontend/dist ./internal/server/dist

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 go build -mod=readonly -trimpath -buildvcs=false \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildDate=${BUILD_DATE}" \
    -o /out/anipick .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=backend /out/anipick /usr/local/bin/anipick
USER nonroot:nonroot
EXPOSE 8787
ENV ANIPICK_ADDR=:8787
ENTRYPOINT ["/usr/local/bin/anipick"]
