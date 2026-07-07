
# go run skips VCS version stamping, so build first
.PHONY: check-commit
check-commit:
	go build -o check-commit .
	./check-commit

.PHONY: update-go-x-deps
update-go-x-deps:
	go get -u golang.org/x/...
