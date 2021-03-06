FROM alpine:3.5

RUN apk add --update ca-certificates # Certificates for SSL

COPY letsrest /bin/letsrest

CMD ["/bin/letsrest"]
