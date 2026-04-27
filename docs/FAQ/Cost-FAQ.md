# Cost FAQ

Evergreen tracks the infrastructure cost of every task across three categories:

- **EC2** — the virtual machine that runs the task. Cost is based on instance type and how long the task runs.
- **EBS** — the disk attached to the EC2 instance. GP3 volumes can be provisioned with higher throughput than the 125 MB/s free tier; the cost above that baseline is tracked.
- **S3** — object storage where task artifacts and logs are uploaded. Both the upload requests (PUT costs) and the ongoing storage are tracked.

All costs use a Finance Team formula with applicable discounts applied. The costs shown in the Evergreen UI cost breakdown modal reflect these discounted values. For programmatic access or additional detail, query the [Evergreen REST API](../API/REST-V2-Usage). See [How can I view cost data via the REST API?](#how-can-i-view-cost-data-via-the-rest-api) below.

## What cost fields does Evergreen track?

Cost is tracked at the task level and rolls up to the version level once all tasks finish.

**Task-level fields** (returned in `task_cost` on the task endpoint):

| Category | Description | API field |
| -------- | ----------- | --------- |
| EC2 | Runtime cost with discounts applied | `adjusted_ec2_cost` |
| EBS | Throughput cost above the 125 MB/s free tier, with discount applied | `adjusted_ebs_throughput_cost` |
| EBS | Storage cost for the attached volume, with discount applied | `adjusted_ebs_storage_cost` |
| S3 | PUT request cost for uploading user artifacts | `adjusted_s3_artifact_put_cost` |
| S3 | Storage cost for uploaded artifacts over their retention period | `adjusted_s3_artifact_storage_cost` |
| S3 | PUT request cost for uploading task log chunks | `adjusted_s3_log_put_cost` |
| S3 | Storage cost for uploaded logs over their retention period | `adjusted_s3_log_storage_cost` |

The `task_cost` object also includes a `total` field which is the sum of all adjusted components. The on-demand (pre-discount) EC2 cost is also available as `on_demand_ec2_cost`.

**Version-level fields** (returned on the version endpoint):

The version aggregates costs across all its tasks once they finish, exposing a single `cost` field. `predicted_cost` is available while tasks are still running.

## How is EC2 cost calculated?

EC2 is the virtual machine that runs the task. The cost is calculated when the task finishes, using the task's actual runtime and the distro's pricing data.

The discounted cost uses a Finance Team formula that blends the savings plan rate and on-demand rate:

```text
adjusted_cost = runtime_seconds * (finance_formula * savings_plan_rate + (1 - finance_formula) * on_demand_rate) / 3600
```

The on-demand cost uses the same formula but applies only the AWS list price for the instance — no savings plan component.

Both discount values and the finance formula are owned by the [BizOps team](https://wiki.corp.mongodb.com/spaces/ADS/pages/143691615/Business+Operations+Team+BizOps).

## How is EBS cost calculated?

EBS is the disk attached to the EC2 instance. GP3 volumes can be provisioned with higher throughput than the 125 MB/s AWS free tier. Only the throughput above that baseline is billable. Pricing is based on us-east-1 rates ($0.04 per MB/s-month) and is prorated to the task's actual runtime.

If a distro has no GP3 mount points, or all volumes are at or below 125 MB/s, the EBS cost is zero.

The discounted cost:

```text
billable_throughput = total_gp3_throughput_MBps - 125
adjusted_ebs_cost   = (billable_throughput * 0.04 / 2_592_000) * runtime_seconds * (1 - ebs_discount)
```

The on-demand cost is the same formula without the `ebs_discount` applied.

## How is S3 cost calculated?

S3 is the object storage where task artifacts and logs are uploaded. Two types of cost are tracked: PUT request costs (charged per upload request) and storage costs (charged for how long the data is retained).

### Artifact PUT cost

Every S3 upload generates one or more PUT API requests depending on file size. Evergreen counts these requests and multiplies by the AWS S3 PUT price ($0.000005 per request), then applies an upload discount.

The number of PUT requests per file:

- Files under 5 MB: 1 PUT request.
- Files 5 MB and over: 1 (initiate) + number of 5 MB parts + 1 (complete). For example, a 12 MB file uses 4 PUT requests.

```text
adjusted_s3_artifact_put_cost = artifact_put_requests * 0.000005 * (1 - upload_cost_discount)
```

The on-demand cost is the same formula without the `upload_cost_discount` applied.

### Artifact storage cost

Storage cost uses S3 Intelligent Tiering pricing, which transitions objects through three tiers based on their retention period:

| Tier              | Days  | Price per GB-month |
| ----------------- | ----- | ------------------ |
| Standard          | 0–30  | $0.023             |
| Infrequent Access | 30–90 | $0.0125            |
| Archive           | 90+   | $0.004             |

The retention period is read from the bucket's S3 lifecycle rule (when possible). If no lifecycle rule is found, the `default_max_artifact_expiration_days` value from the admin settings is used as a fallback. Separate discounts can be configured for each tier.

```text
days_in_standard = min(expiration_days, 30)
days_in_ia       = max(0, min(expiration_days, 90) - 30)
days_in_archive  = max(0, expiration_days - 90)

adjusted_s3_artifact_storage_cost = upload_bytes
    * (days_in_standard * 0.023  * (1 - standard_storage_cost_discount) / GB / 30)
    + (days_in_ia       * 0.0125 * (1 - i_a_storage_cost_discount)      / GB / 30)
    + (days_in_archive  * 0.004  * (1 - archive_storage_cost_discount)   / GB / 30)
```

The on-demand cost is the same formula without any of the tier discounts applied.

### Log PUT cost

Log chunks always use a single PUT request. The cost uses the same rate and discount as artifact PUT costs.

```text
adjusted_s3_log_put_cost = log_put_requests * 0.000005 * (1 - upload_cost_discount)
```

The on-demand cost is the same formula without the `upload_cost_discount` applied.

### Log storage cost

Log storage uses the same Intelligent Tiering formula as artifact storage, applied to the uploaded log bytes and the log bucket's lifecycle rule. The on-demand cost is the same formula without any of the tier discounts applied.

### When are S3 costs calculated?

S3 PUT costs are only calculated if the task uploaded files. S3 storage costs require a resolvable lifecycle rule or a configured `default_max_artifact_expiration_days` fallback.

## How is predicted cost calculated?

Predicted cost provides an EC2 cost estimate before a task finishes. It is computed by averaging the actual EC2 costs (with discounts applied) of the same task — by name, project, and build variant — over the past week.

Predicted cost is available on both the task and version endpoints while tasks are still running. Once all tasks in a version finish, the actual `cost` field is populated and `predicted_cost` reflects the final total.

## How can I view cost data via the REST API?

Cost fields are returned on the task and version endpoints. For full authentication and usage details, see the [REST API documentation](../API/REST-V2-Usage).

**Task** — returns `task_cost` and `predicted_task_cost`. Also returns `s3_usage`, which includes the raw S3 usage data (PUT request counts and upload bytes) that informs the cost calculation.

```text
GET https://evergreen.mongodb.com/rest/v2/tasks/{task_id}
```

**Version** — returns `cost` and `predicted_cost` across all tasks in the version. `cost` is only populated once all tasks have finished running.

```text
GET https://evergreen.mongodb.com/rest/v2/versions/{version_id}
```

Both endpoints require authentication. For full details, see the [REST API documentation](../API/REST-V2-Usage).
