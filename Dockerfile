FROM golang:1.19 AS build
WORKDIR /go/src/github.com/montag451/metaimport
COPY main.go go.* ./
RUN CGO_ENABLED=0 go build

FROM alpine
COPY --from=build /go/src/github.com/montag451/metaimport/metaimport .
ENTRYPOINT ["./metaimport", "/config/config.json"]
