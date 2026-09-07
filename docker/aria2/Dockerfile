FROM alpine:3.23
RUN apk add --no-cache aria2 ca-certificates
COPY docker/aria2/aria2.conf /etc/aria2/aria2.conf
COPY docker/aria2/entrypoint.sh /usr/local/bin/aria2-entrypoint
RUN chmod 755 /usr/local/bin/aria2-entrypoint
VOLUME ["/data", "/config"]
EXPOSE 6800 6888/tcp 6888/udp
ENTRYPOINT ["/usr/local/bin/aria2-entrypoint"]
