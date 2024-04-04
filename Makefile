.PHONY: all compile docker docker-up

all:
	go run main.go

compile:
	go build -o tmm-discord-bot

docker:
	docker build -t tmm-discord-bot .

docker-up:
	docker compose up


help:
	@echo "Builds and runs the Bot"
	@echo "make:		Runs the project (without args, so this does not work)"
	@echo "make compile:	Compiles into a binary"
	@echo "make docker:	Builds the docker image"
	@echo "make docker-up:	Runs docker compose"
