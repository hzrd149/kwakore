run: webview-libs
    cd desktop && go build -o child/napplet ./child && go build -tags napp -o child/napp ./child && go build -o verdana -tags 'dev,novulkan' && WEBVIEW_DEBUG=true VERDANA_SEARCH_DEBUG=1 ./verdana

prod: webview-libs
    cd desktop && go build -o child/napplet ./child && go build -tags napp -o child/napp ./child && go build -o verdana -tags 'novulkan' .

# Install the production desktop launcher into GOBIN (or GOPATH/bin). The
# child webview host must be built first because it is embedded in Verdana.
go-install: webview-libs
    cd desktop && go build -o child/napplet ./child && go build -tags napp -o child/napp ./child && go install -tags 'novulkan' .

# napp windows load libwebview from the launcher's verified per-user dir, so
# the launcher embeds it. The copies come from the pinned go-webview module and
# are git-ignored; every build needs them. GOOS/GOARCH must be unset here:
# go generate builds its copy program for this machine.
webview-libs:
    cd desktop && go generate ./internal/webviewlib

# the ui kit carries the launcher's own face (desktop/assets/*.ttf is
# Verdana, in three faces) as woff2, inlined by backend/webview/embed.go.
# Regenerate them when those ttf files change.
fonts:
	(cd desktop/assets && woff2_compress v.TTF && woff2_compress vb.ttf && woff2_compress vi.ttf)
	cp desktop/assets/v.woff2 desktop/assets/vb.woff2 desktop/assets/vi.woff2 backend/webview/fonts/
	rm -f desktop/assets/*.woff2

# android targets: the aar is rebuilt only when the backend changed (a gomobile
# bind of everything takes a while), the apk takes it from app/libs.
aar:
    mkdir -p android/app/libs
    cd backend && ANDROID_HOME=/opt/android-sdk gomobile bind -target=android -androidapi 26 -o ../android/app/libs/backend.aar ./mobile

apk: aar
    cd android && ./gradlew assembleDebug

install: aar
    cd android && ./gradlew installDebug
