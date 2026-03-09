package junit

type Interface interface {
	AddMessageFailed(name, message, details string)
	AddMessageOK(name, message, details string)
}

type JunitSuiteDummy struct{}

func (*JunitSuiteDummy) AddMessageFailed(_, _, _ string) {
}

func (*JunitSuiteDummy) AddMessageOK(_, _, _ string) {
}
