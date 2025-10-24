# Changelog

## [0.3.0](https://github.com/matzegebbe/k8s-copycat/compare/v0.2.6...v0.3.0) (2025-10-24)


### Features

* honor pod imageid for digest pulls ([967940a](https://github.com/matzegebbe/k8s-copycat/commit/967940a0388bd540dcd87bce2a274bc880ae5710))
* mirror digest pulls using pod image ids ([a63dc13](https://github.com/matzegebbe/k8s-copycat/commit/a63dc1392845f2205be9480521af50330b2a7ad2))


### Bug Fixes

* **mirror:** honor digest pull for manifest lists ([708bda2](https://github.com/matzegebbe/k8s-copycat/commit/708bda28e8036e83e936b01b826ff2e3fe9b27d0))


### Continuous Integration

* gate docker publish on commit tag ([08ca7ea](https://github.com/matzegebbe/k8s-copycat/commit/08ca7ea366ba1655ab81810cbc3fad347d54459d))


### Miscellaneous

* **deps:** bump github.com/aws/aws-sdk-go-v2/service/ecr ([95de82a](https://github.com/matzegebbe/k8s-copycat/commit/95de82a18d322dcf4ddb91ec2401ee8ba4d0d109))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/ecr from 1.50.7 to 1.51.0 ([56f764b](https://github.com/matzegebbe/k8s-copycat/commit/56f764bd9fd66d9bc00661cf958a2f9f1879e953))
* **deps:** bump the go-patch group with 2 updates ([36f6cb6](https://github.com/matzegebbe/k8s-copycat/commit/36f6cb67b07e20820c3299d2a0a1d29138dcb4c2))
* **deps:** bump the go-patch group with 2 updates ([c47df17](https://github.com/matzegebbe/k8s-copycat/commit/c47df17f719def87cea2acd5022560eceec6145b))

## [0.2.6](https://github.com/matzegebbe/k8s-copycat/compare/v0.2.5...v0.2.6) (2025-10-23)


### Bug Fixes

* adjust registry push logging ([7522ec5](https://github.com/matzegebbe/k8s-copycat/commit/7522ec556dbf18d26cba47dcb161d240d84f48e1))
* **logging:** adjust push log verbosity ([e92e5eb](https://github.com/matzegebbe/k8s-copycat/commit/e92e5ebb163916e4045d25fd8d07abaa4fbd9165))
* rename registry exclusion configuration ([ebad7b1](https://github.com/matzegebbe/k8s-copycat/commit/ebad7b11608d552cf854c4b0cafb5277d6872fec))
* rename registry exclusion configuration ([ca22f7d](https://github.com/matzegebbe/k8s-copycat/commit/ca22f7d92a9a5368a7b196abeae74aac052aa5eb))


### Miscellaneous

* **deps:** bump actions/setup-go from 5 to 6 ([d66ce81](https://github.com/matzegebbe/k8s-copycat/commit/d66ce81f44e386e47386534715d59c21db177ef0))
* **deps:** bump actions/setup-go from 5 to 6 ([c6b4778](https://github.com/matzegebbe/k8s-copycat/commit/c6b47787b54bcca19b45e348d427a415b13eb0a7))
* **deps:** bump helm/kind-action from 1.7.0 to 1.12.0 ([09050ea](https://github.com/matzegebbe/k8s-copycat/commit/09050ea966e274c540c3916967a4de7de0a99bf1))
* **deps:** bump helm/kind-action from 1.7.0 to 1.12.0 ([42ff90d](https://github.com/matzegebbe/k8s-copycat/commit/42ff90dd47203e39a14ebc6f2f5bc3971219c8ad))
* **deps:** bump the go-patch group with 2 updates ([0d97f97](https://github.com/matzegebbe/k8s-copycat/commit/0d97f977da7608704a1972ea9cf902bd71c536da))
* **deps:** bump the go-patch group with 2 updates ([36cf003](https://github.com/matzegebbe/k8s-copycat/commit/36cf003fca3187bf3bec33c5ef1491fda5aa555e))

## [0.2.5](https://github.com/matzegebbe/k8s-copycat/compare/v0.2.4...v0.2.5) (2025-10-19)


### Continuous Integration

* add token to release-please workflow configuration ([049442a](https://github.com/matzegebbe/k8s-copycat/commit/049442a4b6d0af110de0bccaf8b1d5a9c2d3c2b6))

## [0.2.4](https://github.com/matzegebbe/k8s-copycat/compare/v0.2.3...v0.2.4) (2025-10-19)


### Bug Fixes

* Update tag pattern for release workflow vx.x.x ([1457870](https://github.com/matzegebbe/k8s-copycat/commit/145787086397af64f4e05faec86e555207f60d43))

## [0.2.3](https://github.com/matzegebbe/k8s-copycat/compare/v0.2.2...v0.2.3) (2025-10-19)


### Continuous Integration

* release on tags ([a722aa9](https://github.com/matzegebbe/k8s-copycat/commit/a722aa9777e0175cb3ade3233b80378408a6aeb4))

## [0.2.2](https://github.com/matzegebbe/k8s-copycat/compare/v0.2.1...v0.2.2) (2025-10-18)


### Miscellaneous

* migrate release process to release-please ([22a0585](https://github.com/matzegebbe/k8s-copycat/commit/22a0585e4edf81d31cbbd58c78159f60390d9b41))
* migrate releases to release-please ([43c3e1c](https://github.com/matzegebbe/k8s-copycat/commit/43c3e1c162a35caf91cadaa62ee6ec0d6d85387f))

## [0.2.1](https://github.com/matzegebbe/k8s-copycat/compare/v0.2.0...v0.2.1) (2025-10-18)


### Bug Fixes

* also create release on api ([a4725b7](https://github.com/matzegebbe/k8s-copycat/commit/a4725b787f4b21f3c9013afce8ead1faad4eb303))
* also create release on api  ([bb6485c](https://github.com/matzegebbe/k8s-copycat/commit/bb6485c93138ac5360709461367c1478a898d81e))

## [0.2.0](https://github.com/matzegebbe/k8s-copycat/compare/v0.1.0...v0.2.0) (2025-10-18)


### Features

* clarify dry pull logging ([48ad2c4](https://github.com/matzegebbe/k8s-copycat/commit/48ad2c446ce9edf5712920ea937ab310b37a9154))

## [0.1.0](https://github.com/matzegebbe/k8s-copycat/compare/v0.0.13...v0.1.0) (2025-10-18)


### Features

* continue force reconcile after partial failures ([e0a54a0](https://github.com/matzegebbe/k8s-copycat/commit/e0a54a00d112c5e3965b045921bdfaedd9ed9cf3))


### Bug Fixes

* add mandatory settings to manifest and log 200 in ci action ([3f02b19](https://github.com/matzegebbe/k8s-copycat/commit/3f02b1988e43b51f249dc17ffed78815d8e47b91))
