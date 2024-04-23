# Changelog

All notable changes to this project will be documented in this file. See [standard-version](https://github.com/conventional-changelog/standard-version) for commit guidelines.

### [0.2.19](https://github.com/awslabs/aws-cloudformation-controller-for-flux/compare/v0.2.18...v0.2.19) (2024-04-23)

### [0.2.18](https://github.com/awslabs/aws-cloudformation-controller-for-flux/compare/v0.2.17...v0.2.18) (2024-04-02)

### [0.2.17](https://github.com/awslabs/aws-cloudformation-controller-for-flux/compare/v0.2.16...v0.2.17) (2024-03-19)

### [0.2.16](https://github.com/awslabs/aws-cloudformation-controller-for-flux/compare/v0.2.15...v0.2.16) (2024-03-05)

### [0.2.15](https://github.com/awslabs/aws-cloudformation-controller-for-flux/compare/v0.2.14...v0.2.15) (2024-02-06)

### [0.2.14](https://github.com/awslabs/aws-cloudformation-controller-for-flux/compare/v0.2.13...v0.2.14) (2024-01-09)

### [0.2.13](https://github.com/awslabs/aws-cloudformation-controller-for-flux/compare/v0.2.12...v0.2.13) (2024-01-03)

### [0.2.12](https://github.com/awslabs/aws-cloudformation-controller-for-flux/compare/v0.2.11...v0.2.12) (2023-11-07)

### [0.2.11](https://github.com/awslabs/aws-cloudformation-controller-for-flux/compare/v0.2.10...v0.2.11) (2023-10-17)

### [0.2.10](https://github.com/awslabs/aws-cloudformation-controller-for-flux/compare/v0.2.9...v0.2.10) (2023-10-03)


### Features

* Upgrade to Flux source controller v1 ([4053f0b](https://github.com/awslabs/aws-cloudformation-controller-for-flux/commit/4053f0bc352269d9c9f5f6c8cfafbf16941b4f71))


### Bug Fixes

* Re-add v1beta2 source controller API to scheme ([2a758a2](https://github.com/awslabs/aws-cloudformation-controller-for-flux/commit/2a758a23597a481802b8bfc3aaabf0a0d4d26875))

### [0.2.9](https://github.com/awslabs/aws-cloudformation-controller-for-flux/compare/v0.2.8...v0.2.9) (2023-09-12)

### [0.2.8](https://github.com/awslabs/aws-cloudformation-controller-for-flux/compare/v0.2.7...v0.2.8) (2023-09-05)

### [0.2.7](https://github.com/awslabs/aws-cloudformation-controller-for-flux/compare/v0.2.6...v0.2.7) (2023-07-11)

### [0.2.6](https://github.com/awslabs/aws-cloudformation-controller-for-flux/compare/v0.2.5...v0.2.6) (2023-06-06)

### [0.2.5](https://github.com/awslabs/aws-cloudformation-controller-for-flux/compare/v0.2.4...v0.2.5) (2023-05-30)

### [0.2.4](https://github.com/awslabs/aws-cloudformation-controller-for-flux/compare/v0.2.3...v0.2.4) (2023-05-16)

### [0.2.3](https://github.com/awslabs/aws-cloudformation-controller-for-flux/compare/v0.2.2...v0.2.3) (2023-05-04)


### Features

* Publish ARM Docker images for the controller ([e1e296d](https://github.com/awslabs/aws-cloudformation-controller-for-flux/commit/e1e296d5fed6d472699706468afce5a25eed0eec)), closes [#32](https://github.com/awslabs/aws-cloudformation-controller-for-flux/issues/32)


### Bug Fixes

* Pull target arch from Docker ([eed3351](https://github.com/awslabs/aws-cloudformation-controller-for-flux/commit/eed3351463555bd70ac2d2c374a80d28b111c469))
* Remove ARM v7 support (not supported by base image) ([2d2034d](https://github.com/awslabs/aws-cloudformation-controller-for-flux/commit/2d2034daa5b84a8befcf5d37e0eea4d7cd0268a7))

### [0.2.2](https://github.com/awslabs/aws-cloudformation-controller-for-flux/compare/v0.2.1...v0.2.2) (2023-04-25)

### [0.2.1](https://github.com/awslabs/aws-cloudformation-controller-for-flux/compare/v0.2.0...v0.2.1) (2023-04-18)


### Bug Fixes

* Add amd64 nodeSelector ([74dfa5b](https://github.com/awslabs/aws-cloudformation-controller-for-flux/commit/74dfa5bc9611ba8ec700fd01f5092ea827c54170))

## [0.2.0](https://github.com/awslabs/aws-cloudformation-controller-for-flux/compare/v0.1.1...v0.2.0) (2023-04-17)


### ⚠ BREAKING CHANGES

* Switch to publishing to production ECR public repo

### Features

* Apply default stack tags to all stacks (cfn-flux-controller/version, cfn-flux-controller/name, cfn-flux-controller/namespace) ([e50fb10](https://github.com/awslabs/aws-cloudformation-controller-for-flux/commit/e50fb1083e60a2cec4885123b70615e9928f3685))
* New aws-region controller flag ([b656ab2](https://github.com/awslabs/aws-cloudformation-controller-for-flux/commit/b656ab2a9bfaabd326df802407b8fa67cd7d2098))
* New stack-tags controller flag for specifying default stack tags ([b6942a3](https://github.com/awslabs/aws-cloudformation-controller-for-flux/commit/b6942a3bbe6cdaf1a035dd0c521e62655dbc29bd))
* New template-bucket controller flag for specifying the S3 bucket to use for storing CloudFormation templates prior to stack deployment ([271da08](https://github.com/awslabs/aws-cloudformation-controller-for-flux/commit/271da08bff27d68a97482fb246235b25c55176f0))
* Specify stack tags in the CloudFormationStack object ([e71683f](https://github.com/awslabs/aws-cloudformation-controller-for-flux/commit/e71683f9002e84192803fb1565865702e426c731))
* Switch to publishing to production ECR public repo ([e0e4c2e](https://github.com/awslabs/aws-cloudformation-controller-for-flux/commit/e0e4c2ea97202cea415017fcca5302e12169f89b))


### Bug Fixes

* Fill in default version if unknown during build ([ccb1940](https://github.com/awslabs/aws-cloudformation-controller-for-flux/commit/ccb19408f964dfcdbc708f245cde5bb9273ddfe6))

### [0.1.1](https://github.com/awslabs/aws-cloudformation-controller-for-flux/compare/v0.1.0...v0.1.1) (2023-04-11)

## [0.1.0](https://github.com/awslabs/aws-cloudformation-controller-for-flux/compare/v0.0.3...v0.1.0) (2023-04-07)

### Features

* Add support for blocking cross-namespace source references ([3feb7f0](https://github.com/awslabs/aws-cloudformation-controller-for-flux/commit/3feb7f0c7ea93498091f9f7df434a577b0abe081))
* Add support for sharding controllers based on labels ([3feb7f0](https://github.com/awslabs/aws-cloudformation-controller-for-flux/commit/3feb7f0c7ea93498091f9f7df434a577b0abe081))
* Add support for the newer digest field for source artifacts ([3feb7f0](https://github.com/awslabs/aws-cloudformation-controller-for-flux/commit/3feb7f0c7ea93498091f9f7df434a577b0abe081))

### Bug Fixes

* Various security fixes found by gosec ([008fe32](https://github.com/awslabs/aws-cloudformation-controller-for-flux/commit/008fe322137090a50d7c1f9cd0f930c7052bda4e))

### [0.0.3](https://github.com/awslabs/aws-cloudformation-controller-for-flux/compare/v0.0.2...v0.0.3) (2023-04-05)


### Features

* Add support for declaring stack dependencies ([a2ffd37](https://github.com/awslabs/aws-cloudformation-controller-for-flux/commit/a2ffd37bf0c3ac45760f33018e0977fe3aa62965))
* Add support for stack parameters ([465b1c8](https://github.com/awslabs/aws-cloudformation-controller-for-flux/commit/465b1c8933304a2a74471062a7ccd7a82c3cee5e))

### [0.0.2](https://github.com/awslabs/aws-cloudformation-controller-for-flux/compare/v0.0.1...v0.0.2) (2023-04-04)

Working CloudFormation controller with support for declaring a CloudFormationStack object with a stack name and
a Flux source reference for the stack's CloudFormation template.

### 0.0.1 (2023-03-03)

Beginning of this project
