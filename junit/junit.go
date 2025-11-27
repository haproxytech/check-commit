package junit

type Interface interface {
	AddMessageFailed(name, message, details string)
	AddMessageOK(name, message, details string)
}

type JunitSuiteDummy struct{}

func (j *JunitSuiteDummy) AddMessageFailed(name, message, details string) {
}

func (j *JunitSuiteDummy) AddMessageOK(name, message, details string) {
}
