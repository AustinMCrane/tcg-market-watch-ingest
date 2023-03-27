how to build docker image:
```
docker build -t tcg-market-watch-ingest --build-arg SSH_PRIVATE_KEY="$(cat ~/.ssh/id_rsa)" .
```
