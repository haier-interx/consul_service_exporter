FROM golang:1.8.3-alpine3.6
MAINTAINER Xue Bing <xuebing1110@gmail.com>

# repo
RUN cp /etc/apk/repositories /etc/apk/repositories.bak
RUN echo "http://mirrors.aliyun.com/alpine/v3.6/main/" > /etc/apk/repositories
RUN echo "http://mirrors.aliyun.com/alpine/v3.6/community/" >> /etc/apk/repositories

# timezone
RUN apk update
RUN apk add --no-cache tzdata \
    && echo "Asia/Shanghai" > /etc/timezone \
    && ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime

# move to GOPATH
RUN mkdir -p /go/src/github.com/haier-interx/consul_service_exporter
COPY . $GOPATH/src/github.com/haier-interx/consul_service_exporter/
WORKDIR $GOPATH/src/github.com/haier-interx/consul_service_exporter

# copy config
RUN mkdir -p /app

# build
RUN go build -o /app/consul_service_exporter main.go

EXPOSE 9111
WORKDIR /app
CMD ["/app/consul_service_exporter"]