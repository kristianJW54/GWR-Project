FROM ubuntu:latest
LABEL authors="Kristian"

ENTRYPOINT ["top", "-b"]