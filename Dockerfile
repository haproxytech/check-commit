FROM golang:alpine AS builder
# git is required for VCS version stamping (debug.ReadBuildInfo)
RUN apk --no-cache add git
RUN mkdir /build
ADD . /build/
WORKDIR /build
RUN go build -buildvcs=true -o check

FROM alpine:latest
RUN apk --no-cache add aspell aspell-en
COPY --from=builder /build/check /check
WORKDIR /
ENTRYPOINT ["/check"]
