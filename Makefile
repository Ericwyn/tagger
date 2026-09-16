.PHONY: all build build-frontend dev-backend dev-frontend dev-frontend-mock \
	test test-go test-frontend test-integration lint clean

BINARY ?= tagger
VERSION ?= 1.0.4
MUSIC_DIR ?=

all: build

build-frontend:
	cd frontend && npm ci && npm run build
	rm -rf web/dist
	mkdir -p web/dist
	cp -R frontend/dist/. web/dist/
	touch web/dist/.gitkeep

build: build-frontend
	mkdir -p dist
	CGO_ENABLED=0 go build -trimpath \
		-ldflags "-X github.com/ericwyn/tagger/internal/version.Version=$(VERSION)" \
		-o dist/$(BINARY) ./cmd/tagger

dev-backend:
	@test -n "$(MUSIC_DIR)" || (echo "MUSIC_DIR is required, for example: make dev-backend MUSIC_DIR=/path/to/music" && exit 2)
	go run ./cmd/tagger --music-dir "$(MUSIC_DIR)"

dev-frontend:
	cd frontend && npm run dev

dev-frontend-mock:
	cd frontend && VITE_API_MODE=mock npm run dev

test: test-go test-frontend

test-go:
	go test ./...

test-frontend:
	cd frontend && npm test

test-integration:
	@test -n "$(MUSIC_DIR)" || (echo "MUSIC_DIR is required" && exit 2)
	TAGGER_TEST_MUSIC_DIR="$(MUSIC_DIR)" go test ./cmd/tagger ./internal/scanner ./internal/filewrite ./internal/library ./internal/server \
		-run 'TestAudioAPIWithCopiedTestMusic|TestBatchEditWorkerWithCopiedTestMusic|TestProbeTestMusicCorpus|TestScannerReadsTestMusicCorpus|TestWriterWithCopiedTestMusicMP3AndFLAC|TestWriterWithCopiedTestMusicArtwork|TestSuccessfulRealTagWriteCreatesPersistentRevision|TestSuccessfulRealSidecarWriteCreatesPersistentRevision' -count=1 -v

lint:
	go vet ./...
	cd frontend && npm run lint

clean:
	rm -rf dist frontend/dist web/dist
	mkdir -p web/dist
	touch web/dist/.gitkeep
