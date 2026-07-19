help:
	@echo "To build the API protobuf files run 'make api'"

api:
	(cd api; protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    *.proto)
	(cd client-cc/src/protobuf; protoc --cpp_out=. \
		--grpc_out=. \
		--plugin=protoc-gen-grpc=`which grpc_cpp_plugin` \
		--proto_path=../../../api ../../../api/*.proto)

.PHONY: api help
