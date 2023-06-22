how to build docker image:
```
docker build -t tcgplayer-ingest --build-arg SSH_PRIVATE_KEY="$(cat ~/.ssh/id_rsa)" .
```
