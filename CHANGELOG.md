# Changelog

All notable changes to this project will be documented in this file. See [standard-version](https://github.com/conventional-changelog/standard-version) for commit guidelines.

## [0.1.0](https://github.com/awslabs/aws-cloudformation-controller-for-flux/compare/v0.0.3...v0.1.0) (2023-04-07)


### ⚠ BREAKING CHANGES

* Add support for blocking cross-namespace source references, for sharding controllers based on labels, and for the newer digest field for source artifacts

### Features

* Add support for blocking cross-namespace source references, for sharding controllers based on labels, and for the newer digest field for source artifacts ([3feb7f0](https://github.com/awslabs/aws-cloudformation-controller-for-flux/commit/3feb7f0c7ea93498091f9f7df434a577b0abe081))


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
