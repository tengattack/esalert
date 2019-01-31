NAME=esalert
VERSION=1.0.0
REGISTRY_PREFIX=$(if $(REGISTRY),$(addsuffix /, $(REGISTRY)))

.PHONY: build update

build:
	docker build --build-arg version=${VERSION} \
		--build-arg go_get_http_proxy=${GO_GET_HTTP_PROXY} \
		-t ${NAME}:${VERSION} .

publish:
	docker tag ${NAME}:${VERSION} ${REGISTRY_PREFIX}${NAME}:${VERSION}
	docker push ${REGISTRY_PREFIX}${NAME}:${VERSION}
