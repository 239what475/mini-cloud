FROM alpine:3.22

ARG CONTROL_PLANE_CONFIG=deploy/container/control-plane.yaml.example

WORKDIR /opt/mini-cloud

COPY dist/release/linux-amd64/control-plane /usr/local/bin/control-plane
COPY web/dist ./web
COPY ${CONTROL_PLANE_CONFIG} /etc/mini-cloud/control-plane.yaml

EXPOSE 9000

ENTRYPOINT ["/usr/local/bin/control-plane"]
CMD ["--config", "/etc/mini-cloud/control-plane.yaml"]
