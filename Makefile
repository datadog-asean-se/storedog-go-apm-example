NAMESPACE ?= storedog
IMAGE     ?= storedog-go-shop:latest
OVERLAY   ?= base

.PHONY: deploy build status logs destroy
deploy:        ## Build the image, load it into the cluster, deploy to $(NAMESPACE)
	IMAGE=$(IMAGE) NAMESPACE=$(NAMESPACE) OVERLAY=$(OVERLAY) ./scripts/deploy.sh
build:         ## Build the image only
	DOCKER_BUILDKIT=1 docker build -t $(IMAGE) .
status:        ## Show pods and recent gateway logs
	kubectl -n $(NAMESPACE) get pods -l 'app in (shop-gateway,shop-inventory,shop-payment,shop-loadgen)'
logs:          ## Tail gateway logs (JSON, with dd.trace_id)
	kubectl -n $(NAMESPACE) logs deploy/shop-gateway --tail=20 -f
destroy:       ## Remove everything this repo deployed
	kubectl -n $(NAMESPACE) delete -k deploy/base --ignore-not-found
