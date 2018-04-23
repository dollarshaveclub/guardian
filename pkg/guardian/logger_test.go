package guardian

import (
	"io/ioutil"

	"github.com/sirupsen/logrus"
)

var NullLogger = &logrus.Logger{Out: ioutil.Discard}
