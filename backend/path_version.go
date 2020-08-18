package backend

import (
	"context"

	"github.com/hashicorp/vault/sdk/framework"
	"github.com/hashicorp/vault/sdk/logical"
)

func versionPaths(b *backend) []*framework.Path {
	return []*framework.Path{
		&framework.Path{
			Pattern:         "version",
			HelpSynopsis:    "Shows app version",
			HelpDescription: ``,
			Fields:          map[string]*framework.FieldSchema{},
			ExistenceCheck:  b.pathExistenceCheck,
			Callbacks: map[logical.Operation]framework.OperationFunc{
				logical.ReadOperation: b.pathVersion,
			},
		},
	}
}

func (b *backend) pathVersion(ctx context.Context, req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
	return &logical.Response{
		Data: map[string]interface{}{
			"version": b.Version,
		},
	}, nil
}
