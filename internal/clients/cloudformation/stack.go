// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package cloudformation

import (
	"bytes"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
)

// Stack represents a AWS CloudFormation stack.
type Stack struct {
	Name       string
	Region     string
	Generation int64
	*stackConfig
}

type stackConfig struct {
	TemplateBody string
	TemplateURL  string
	Parameters   []types.Parameter
	Tags         []types.Tag
}

// StackOption allows you to initialize a Stack with additional properties.
type StackOption func(s *Stack)

// NewStack creates a stack with the given name and template body.
func NewStack(name string, template *bytes.Buffer, opts ...StackOption) *Stack {
	s := &Stack{
		Name: name,
		stackConfig: &stackConfig{
			TemplateBody: template.String(),
		},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// NewStackWithURL creates a stack with a URL to the template.
func NewStackWithURL(name string, templateURL string, opts ...StackOption) *Stack {
	s := &Stack{
		Name: name,
		stackConfig: &stackConfig{
			TemplateURL: templateURL,
		},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithParameters passes parameters to a stack.
func WithParameters(params map[string]string) StackOption {
	return func(s *Stack) {
		var flatParams []types.Parameter
		for k, v := range params {
			flatParams = append(flatParams, types.Parameter{
				ParameterKey:   aws.String(k),
				ParameterValue: aws.String(v),
			})
		}
		s.Parameters = flatParams
	}
}

// WithTags applies the tags to a stack.
func WithTags(tags map[string]string) StackOption {
	return func(s *Stack) {
		var flatTags []types.Tag
		for k, v := range tags {
			flatTags = append(flatTags, types.Tag{
				Key:   aws.String(k),
				Value: aws.String(v),
			})
		}
		s.Tags = flatTags
	}
}

// StackEvent is an alias the SDK's StackEvent type.
type StackEvent types.StackEvent

// StackDescription is an alias the SDK's Stack type.
type StackDescription types.Stack

// StackResource is an alias the SDK's StackResource type.
type StackResource types.StackResource

// SDK returns the underlying struct from the AWS SDK.
func (d *StackDescription) SDK() *types.Stack {
	raw := types.Stack(*d)
	return &raw
}
