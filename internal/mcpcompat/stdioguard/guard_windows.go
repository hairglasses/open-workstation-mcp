package stdioguard

import "os"

func GuardStdio() (*os.File, func(), error) {
	return os.Stdout, func() {}, nil
}
