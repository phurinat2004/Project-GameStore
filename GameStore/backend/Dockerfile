FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /gamestore-api .

FROM alpine:3.21
RUN adduser -D -H appuser
COPY --from=build /gamestore-api /usr/local/bin/gamestore-api
USER appuser
EXPOSE 8080
ENTRYPOINT ["gamestore-api"]
