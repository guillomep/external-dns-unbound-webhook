FROM gcr.io/distroless/static-debian12:nonroot

USER 20000:20000
ADD --chmod=555 external-dns-unbound-webhook /opt/external-dns-unbound-webhook/app

ENTRYPOINT ["/opt/external-dns-unbound-webhook/app"]
