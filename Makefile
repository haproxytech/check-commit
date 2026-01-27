
.PHONY: check-commit
check-commit:
	go run .

.PHONY: update-go-x-deps
update-go-x-deps:
	go get -u golang.org/x/...
