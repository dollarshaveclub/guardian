package guardian

import (
	"io/ioutil"

	"github.com/sirupsen/logrus"
)

var TestingLogger = &logrus.Logger{Out: ioutil.Discard}
