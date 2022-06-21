//go:generate mockgen -package client -destination=mocks.go sigs.k8s.io/controller-runtime/pkg/client Client

package client

import _ "github.com/golang/mock/mockgen/model"
