gql:
	go run github.com/99designs/gqlgen generate

generate: mocks gql

create-migration:
	go run github.com/golang-migrate/migrate/v4/cmd/migrate create -ext sql -dir db/migrations $(name)

migrate:
	go run cmd/main.go migrate up

migrate-create:
	migrate create -ext sql -dir db/migrations -seq $(name)


mocks:
	echo "Generating mocks"
test:
	go test -count=1 ./...

# The integration packages carry the `integration` build tag.
test-integration:
	cd integration-tests && go test -tags integration -count=1 -v ./...
