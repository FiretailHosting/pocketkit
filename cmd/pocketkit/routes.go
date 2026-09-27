package main

import (
	"fmt"
	"strings"
)

func cmdRoutes(args []string) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}

	res, _, err := generate(dir)
	if err != nil {
		return err
	}

	if len(res.Routes) == 0 && len(res.Hooks) == 0 {
		fmt.Println("No routes or hooks found. Add a file at api/<path>/GET.go to make one.")
		return nil
	}

	if len(res.Routes) > 0 {
		w := newTabWriter()
		fmt.Fprintln(w, "METHOD\tPATH\tAUTH\tSOURCE")
		for _, r := range res.Routes {
			auth := "required"
			if r.Public {
				auth = "public"
			}
			if r.Middlewares != "" {
				auth += " +mw"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Method, r.Path, auth, r.File)
		}
		w.Flush()
	}

	if len(res.Hooks) > 0 {
		fmt.Println()
		w := newTabWriter()
		fmt.Fprintln(w, "HOOK\tSCOPE\tSOURCE")
		for _, h := range res.Hooks {
			scope := strings.Join(h.Tags, ", ")
			if scope == "" {
				scope = "(app)"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", h.Name, scope, h.File)
		}
		w.Flush()
	}

	return nil
}
