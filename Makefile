
all:
	go run main.go

compile:
	go build main.go

help:
	@echo "Builds and runs the Bot"
	@echo "make:		Runs the project"
	@echo "make compile:	Compiles into a binary"
