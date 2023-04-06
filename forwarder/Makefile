NAME:=kudzu-forwarder

all: $(NAME)-amd64 $(NAME)-rpi $(NAME)-arm6 $(NAME)-arm7 $(NAME)-mipsle-24kec-v5 $(NAME)-mips-24kec-v5

tarball: $(NAME)-all.tar.gz

$(NAME)-all.tar.gz: $(NAME)-amd64 $(NAME)-rpi $(NAME)-arm6 $(NAME)-arm7 $(NAME)-mipsle-24kec-v5 $(NAME)-mips-24kec-v5
	tar -zcf $@ $^

$(NAME)-amd64: *.go
	GOOS=linux GOARCH=amd64 go build -o $@ -a -gcflags=all="-l -B" -ldflags="-s -w"
	# upx -9 $@
	tar -zcf $@.tgz $@

$(NAME)-rpi: *.go
	GOOS=linux GOARCH=arm GOARM=5 go build -o $@ -a -gcflags=all="-l -B" -ldflags="-s -w"
	# upx -9 $@
	tar -zcf $@.tgz $@

$(NAME)-arm6: *.go
	GOOS=linux GOARCH=arm GOARM=6 go build -o $@ -a -gcflags=all="-l -B" -ldflags="-s -w"
	# upx -9 $@
	tar -zcf $@.tgz $@

$(NAME)-arm7: *.go
	GOOS=linux GOARCH=arm GOARM=7 go build -o $@ -a -gcflags=all="-l -B" -ldflags="-s -w"
	# upx -9 $@
	tar -zcf $@.tgz $@

$(NAME)-mipsle-24kec-v5: *.go
	GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build -o $@ -a -gcflags=all="-l -B" -ldflags="-s -w"
	# upx -9 $@
	tar -zcf $@.tgz $@

$(NAME)-mips-24kec-v5: *.go
	GOOS=linux GOARCH=mips GOMIPS=softfloat go build -o $@ -a -gcflags=all="-l -B" -ldflags="-s -w"
	# upx -9 $@
	tar -zcf $@.tgz $@

.PHONY: all