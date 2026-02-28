FROM golang:1.25-alpine3.22 AS build

ARG VERSION="untracked"

RUN apk --update add ca-certificates

WORKDIR /warehouse/

RUN go env -w GOPROXY=https://goproxy.cn,direct

COPY ./go.mod ./
COPY ./go.sum ./
RUN go mod download

COPY . /warehouse/
RUN go build -o main -trimpath -ldflags="-s -w -X 'main.version=$VERSION'" ./cmd/server

FROM scratch

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /warehouse/main /bin/warehouse

EXPOSE 6065

ENTRYPOINT [ "warehouse" ]
