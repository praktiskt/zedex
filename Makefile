get-protos:
	bash hack/get_protos.sh

.PHONY=build
build:
	sh hack/build.sh

.PHONY=build-docker
build-docker:
	docker build -t zedex:build .

.PHONY=push-docker
push-docker: build-docker
	docker tag zedex:build praktiskt/zedex:latest &&\
	docker push praktiskt/zedex:latest
