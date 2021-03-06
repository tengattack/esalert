NAME=esalert
VERSION=1.0.2
REGISTRY_PREFIX=$(if $(REGISTRY),$(addsuffix /, $(REGISTRY)))

.PHONY: build update

build:
	docker build --build-arg version=${VERSION} \
		--build-arg proxy=${BUILD_HTTP_PROXY} \
		--build-arg goproxy=${GOPROXY} \
		-t ${NAME}:${VERSION} .

publish:
	docker tag ${NAME}:${VERSION} ${REGISTRY_PREFIX}${NAME}:${VERSION}
	docker push ${REGISTRY_PREFIX}${NAME}:${VERSION}
