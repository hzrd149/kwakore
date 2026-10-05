package backend

import "errors"

// ─── launcher error lines ───────────────────────────────────────

// The launcher's error line (State.FetchErr) is fixed copy (05-UI-SPEC
// copy rules): it never shows a raw Go error, a blob hash, a server URL, a
// file path, an event id or a full address, whose d is author text. Those
// go to the log. A napp is named by its sanitized display name only.

// errNotInstalled is an install-only operation (update, launch, a shortcut
// entry) on a napp that is not installed, or no longer is.
var errNotInstalled = errors.New("that napp is not installed")

// errFilesFailed is an install or update that stopped because one of the
// manifest's files could not be downloaded or did not match its hash, or
// the download ran out of time. It wraps the download error, which names
// the hash and the servers.
var errFilesFailed = errors.New("a file couldn't be downloaded or didn't match its manifest")

// Fallback phrases for errors the launcher has no words of its own for.
const (
	installFallback   = "its files couldn't be saved"
	windowFallback    = "its window couldn't be opened"
	shortcutFallback  = "its actions couldn't be run"
	trialDataFallback = "the data couldn't be saved"
	addressFallback   = "it isn't a napp address or couldn't be found"
)

// fixedErrors are the errors whose own text is fixed copy, safe to show.
var fixedErrors = []error{
	errUnavailable,
	errBusy,
	errOlderVersion,
	errNotInstalled,
	errFilesFailed,
	errTrialFiles,
}

// failureLine is the launcher error line for a failed operation: prefix,
// then the text of the fixed error err is (or wraps), else fallback. The
// raw error goes to the log at Warn, with id, so nothing is lost.
func failureLine(prefix, fallback, id string, err error) string {
	line := prefix + fallback
	for _, known := range fixedErrors {
		if errors.Is(err, known) {
			line = prefix + known.Error()
			break
		}
	}
	log.Warn().Err(err).Str("napp", id).Str("shown", line).Msg("failure shown in the launcher")
	return line
}

// OpenAddressFailure is the launcher error line for an OpenAddress that
// failed, for a GUI module that shows it (the gomobile binding).
func OpenAddressFailure(err error) string {
	return failureLine("couldn't open that address: ", addressFallback, "", err)
}

// fetchErrName is n's name in a launcher error line: its title, else its
// d, cleaned like a notice name, and never its id (a full address).
func fetchErrName(n Napp) string {
	if out := noticeText(n.Name); out != "" {
		return out
	}
	if out := noticeText(n.D); out != "" {
		return out
	}
	if n.IsNapplet() {
		return "Unnamed napplet"
	}
	return "Unnamed napp"
}
