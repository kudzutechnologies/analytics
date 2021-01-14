
help:
	@echo "To build the API protobuf files run 'make api'"

api:
	(cd api; protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    *.proto)

.PHONY: api help