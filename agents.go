package main

import (
	"errors"
	"strconv"

	"github.com/spf13/cobra"
)

func addAgents(r *cobra.Command) {
	r.AddCommand(&cobra.Command{
		Use:   "agents",
		Short: "List account agents available for assignment",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, a []string) error {
			account := opt.account
			if account == "" {
				account = "1"
			}
			id, err := strconv.ParseUint(account, 10, 64)
			if err != nil || id == 0 {
				return errors.New("account ID must be a positive integer")
			}
			v, err := request(c.Context(), "GET", "/api/v1/accounts/"+account+"/agents", nil, nil, "")
			if err != nil {
				return err
			}
			return emit(v)
		},
	})
}
