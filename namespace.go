package main

import (
	"fmt"
)

// resources type
type resources struct {
	CPU    string `yaml:"cpu,omitempty"`
	Memory string `yaml:"memory,omitempty"`
}

// limits type
type limits []struct {
	Max                  resources `yaml:"max,omitempty"`
	Min                  resources `yaml:"min,omitempty"`
	Default              resources `yaml:"default,omitempty"`
	DefaultRequest       resources `yaml:"defaultRequest,omitempty"`
	MaxLimitRequestRatio resources `yaml:"maxLimitRequestRatio,omitempty"`
	LimitType            string    `yaml:"type"`
}

// namespace type represents the fields of a namespace
type namespace struct {
	Protected              bool              `yaml:"protected"`
	Limits                 limits            `yaml:"limits,omitempty"`
	Labels                 map[string]string `yaml:"labels"`
	Annotations            map[string]string `yaml:"annotations"`
}

// checkNamespaceDefined checks if a given namespace is defined in the namespaces section of the desired state file
func checkNamespaceDefined(ns string, s state) bool {
	_, ok := s.Namespaces[ns]
	if !ok {
		return false
	}
	return true
}

// print prints the namespace
func (n namespace) print() {
	fmt.Println("")
	fmt.Println("\tprotected : ", n.Protected)
	fmt.Println("\tlabels : ")
	printMap(n.Labels, 2)
	fmt.Println("------------------- ")
}
