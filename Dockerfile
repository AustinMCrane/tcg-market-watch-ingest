FROM golang:1.20-alpine

WORKDIR /tcg-market-watch-ingest
RUN apk add --update --no-cache ca-certificates git bash openssh-client build-base

ARG SSH_PRIVATE_KEY
RUN mkdir -p /root/.ssh \
  && echo "$SSH_PRIVATE_KEY" > /root/.ssh/id_rsa \
  && git config --global url."git@github.com:".insteadOf https://github.com \
  && ssh-keyscan github.com >> /root/.ssh/known_hosts \
  && chmod 600 /root/.ssh/id_rsa
ENV GOPRIVATE=github.com/AustinMCrane


COPY go.mod ./
COPY go.sum ./

COPY . ./

RUN go mod download
RUN go build -o /tcg-market-watch-ingest

ENTRYPOINT [ "/tcg-market-watch-ingest" ]
