# Dev loop
make redis-up          # start Redis in Docker
make dev               # hot-reload the app

# Quality
make check             # tidy + fmt + vet + lint + test
make test-cover        # tests + HTML coverage report
make bench             # benchmarks

# Build & ship
make build             # local binary
make build-linux       # cross-compile
make docker-build      # docker image
make up                # full stack via compose

# Cleanup
make redis-down
make down
make clean