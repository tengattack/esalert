NAME=esalert
VERSION=1.0.0
REGISTRY_PREFIX=$(if $(REGISTRY),$(addsuffix /, $(REGISTRY)))

.PHONY: build update rollback create publish

build:
	docker build --build-arg version=${VERSION} \
		--build-arg go_get_http_proxy=${GO_GET_HTTP_PROXY} \
		-t ${NAME}:${VERSION} .

publish:
	docker tag ${NAME}:${VERSION} ${REGISTRY_PREFIX}${NAME}:${VERSION}
	docker push ${REGISTRY_PREFIX}${NAME}:${VERSION}

update:
	docker service update --image ${NAME}:${VERSION} ${NAME}

rollback:
	docker service rollback ${NAME}

create:
	docker service create --replicas 1 \
		--env "HOST={{.Node.Hostname}}" \
		--update-order stop-first \
		--rollback-order stop-first \
		--mount type=bind,source=/etc/esalert,destination=/etc/esalert,readonly \
		--name ${NAME} ${REGISTRY_PREFIX}${NAME}:${VERSION}
