FROM gcr.io/distroless/static-debian11:nonroot
ENTRYPOINT ["/baton-zoom"]
COPY baton-zoom /