# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$(go env GOARCH) go build -o /out/doppler ./cmd/manager

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=build /out/doppler /doppler
USER 65532:65532
ENTRYPOINT ["/doppler"]
