# Cross-account Amazon ECR access with IRSA

k8s-copycat relies on the standard AWS SDK credential chain. When you deploy the controller with IAM Roles for Service Accounts (IRSA), the pod receives temporary credentials, and the SDK can assume additional roles (for example, a central ECR writer role) without any extra logic in k8s-copycat.

This guide explains how to mirror images into a central Amazon ECR registry that lives in a different AWS account by chaining roles through IRSA.

## Prerequisites

* An Amazon EKS cluster in each workload account that runs k8s-copycat.
* An Amazon ECR registry in a central account.
* An EKS OIDC identity provider configured for each cluster.
* Familiarity with creating IAM roles and policies.

## Reference architecture

The example below uses three accounts:

1. **Account A (111111111111)** – hosts the shared ECR registry and exposes an IAM role called `CentralECRPushRole`.
2. **Account B (222222222222)** – runs an EKS cluster with k8s-copycat in the `k8s-copycat` namespace.
3. **Account C (333333333333)** – another EKS cluster following the same pattern as Account B.

### Account A – central registry role

Create an IAM role named `CentralECRPushRole` and attach the minimal ECR permissions. The `Sid` value is just a label.

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "EcrPush",
      "Effect": "Allow",
      "Action": [
        "ecr:GetAuthorizationToken",
        "ecr:BatchCheckLayerAvailability",
        "ecr:CompleteLayerUpload",
        "ecr:InitiateLayerUpload",
        "ecr:PutImage",
        "ecr:UploadLayerPart",
        "ecr:DescribeRepositories",
        "ecr:DescribeImages"
      ],
      "Resource": "arn:aws:ecr:us-east-1:111111111111:repository/central-apps/*"
    }
  ]
}
```

Trust this role to the IRSA roles in each workload account:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "AllowPartnerAccounts",
      "Effect": "Allow",
      "Principal": {
        "AWS": [
          "arn:aws:iam::222222222222:role/k8s-copycat-controller",
          "arn:aws:iam::333333333333:role/k8s-copycat-controller"
        ]
      },
      "Action": "sts:AssumeRole"
    }
  ]
}
```

Onboard additional workload accounts by adding their IRSA role ARNs to the `Principal` list.

### Account B – EKS namespace and IRSA role

Create an IAM role named `k8s-copycat-controller`. Grant it permission to assume the central role:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": "sts:AssumeRole",
      "Resource": "arn:aws:iam::111111111111:role/CentralECRPushRole"
    }
  ]
}
```

Configure the trust policy so only the `k8s-copycat-controller` service account in the `k8s-copycat` namespace can assume it through the cluster's OIDC provider. Replace the provider ARN and ID with your cluster-specific values.

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws:iam::222222222222:oidc-provider/oidc.eks.us-east-1.amazonaws.com/id/EXAMPLED539D4633E53DE1B716D3041E"
      },
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "StringEquals": {
          "oidc.eks.us-east-1.amazonaws.com/id/EXAMPLED539D4633E53DE1B716D3041E:sub": "system:serviceaccount:k8s-copycat:k8s-copycat-controller"
        }
      }
    }
  ]
}
```

Annotate the controller's service account inside the EKS cluster:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: k8s-copycat-controller
  namespace: k8s-copycat
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::222222222222:role/k8s-copycat-controller
```

### Account C – replicate the workload pattern

Provision an IAM role named `k8s-copycat-controller` in Account C with the same policy that allows `sts:AssumeRole` on `CentralECRPushRole`. Update its trust policy to reference Account C's OIDC provider and use the same subject condition (`system:serviceaccount:k8s-copycat:k8s-copycat-controller`). Annotate the service account in the `k8s-copycat` namespace with the new role ARN.

### Controller configuration

In each workload account, configure k8s-copycat with the central registry details. Environment variables shown here are equivalent to setting the same values in the controller's config file. If your service account already sets `AWS_ROLE_ARN` for IRSA, you can keep it and use `ecr.assumeRoleArn` to tell copycat which role to assume for ECR access.

```
ECR_ACCOUNT_ID=111111111111
ECR_REPO_PREFIX=central-apps/
AWS_REGION=us-east-1
AWS_ROLE_ARN=arn:aws:iam::111111111111:role/CentralECRPushRole   # optional helper
AWS_WEB_IDENTITY_TOKEN_FILE=/var/run/secrets/eks.amazonaws.com/serviceaccount/token
```

```yaml
ecr:
  accountID: "111111111111"
  region: "us-east-1"
  repoPrefix: "central-apps/"
  assumeRoleArn: "arn:aws:iam::111111111111:role/CentralECRPushRole"
```

With this setup, IRSA injects the base credentials into the k8s-copycat pods. The AWS SDK automatically assumes `CentralECRPushRole`, enabling both workload accounts to mirror images into the central ECR registry without modifying repository resource policies.
