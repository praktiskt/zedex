.PHONY=build
build:
	sh hack/build.sh

.PHONY=build-docker
build-docker:
	docker build -t zedex:build .
