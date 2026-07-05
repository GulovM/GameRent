package clock

import "time"

type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type RealClock struct{}

func NewRealClock() Clock {
	return &RealClock{}
}

func (RealClock) Now() time.Time {
	return time.Now()
}

func (RealClock) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}

type MockClock struct {
	currentTime time.Time
}

func NewMockClock(t time.Time) *MockClock {
	return &MockClock{currentTime: t}
}

func (m *MockClock) Now() time.Time {
	return m.currentTime
}

func (m *MockClock) After(d time.Duration) <-chan time.Time {

	ch := make(chan time.Time, 1)
	ch <- m.currentTime.Add(d)
	return ch
}

func (m *MockClock) SetNow(t time.Time) {
	m.currentTime = t
}

func (m *MockClock) Add(d time.Duration) {
	m.currentTime = m.currentTime.Add(d)
}
