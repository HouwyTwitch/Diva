FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY cmd/server ./cmd/server
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /diva ./cmd/server

FROM scratch
COPY --from=build /diva /diva
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/diva"]
