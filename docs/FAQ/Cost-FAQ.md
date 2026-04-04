# Cost FAQ

## What cost fields does Evergreen track for a task?

Each task tracks the following cost components:

- **EC2 on-demand cost**: The runtime cost of the instance at AWS list price.
- **EC2 adjusted cost**: The runtime cost using the finance formula, which blends the savings plan rate and on-demand rate. This is the figure used for internal cost attribution.
- **EBS GP3 throughput cost — on-demand**: The cost of EBS GP3 throughput above the 125 MB/s free tier at AWS list price.
- **EBS GP3 throughput cost — adjusted**: The same EBS throughput cost with an internal discount applied.
- **S3 Artifact PUT Cost**: The cost of S3 PUT API requests made when uploading user artifacts via `s3.put` commands.
- **S3 Log PUT Cost**: The cost of S3 PUT API requests made when uploading task log chunks.
- **S3 Artifact Storage Cost**: The cost of storing uploaded artifact bytes over their retention period, calculated using S3 Intelligent Tiering pricing.
- **S3 Log Storage Cost**: The cost of storing uploaded log bytes over their retention period, calculated using S3 Intelligent Tiering pricing.

These costs roll up to the version level when the version finishes.

## How is the EC2 cost calculated?

EC2 costs are calculated when a task finishes, using the task's actual runtime and the distro's pricing data.

The **on-demand cost** uses the AWS list price for the distro:

```text
on_demand_cost = runtime_seconds * (on_demand_rate / 3600) * (1 - on_demand_discount)
```

The **adjusted cost** blends the savings plan rate and on-demand rate using the finance formula:

```text
adjusted_cost = runtime_seconds * (finance_formula * savings_plan_rate + (1 - finance_formula) * on_demand_rate) / 3600
```

Both discount values and the finance formula are configured in the `cost` section of the Evergreen admin settings.

## How is the EBS throughput cost calculated?

EBS GP3 throughput cost applies to tasks running on distros with GP3 volumes configured above the 125 MB/s free tier. Only the throughput above 125 MB/s is billable. Pricing is based on us-east-1 rates ($0.04 per MB/s-month) and is prorated to the task's actual runtime.

If a distro has no GP3 mount points, or all volumes are at or below the 125 MB/s baseline, the EBS cost is zero.

The **on-demand cost** uses the AWS list price:

```text
billable_throughput    = total_gp3_throughput_MBps - 125
on_demand_ebs_cost     = (billable_throughput * 0.04 / 2_592_000) * runtime_seconds
```

The **adjusted cost** applies the EBS discount:

```text
adjusted_ebs_cost = on_demand_ebs_cost * (1 - ebs_discount)
```

## How is the S3 PUT cost calculated?

Every S3 upload generates one or more PUT API requests depending on file size and upload method. Evergreen counts these requests and multiplies by the AWS S3 PUT price ($0.000005 per request), then applies an upload discount from the admin settings.

The number of PUT requests per file depends on whether the upload is a single-part or multipart transfer:

- Files under 5 MB: 1 PUT request.
- Files 5 MB and over: 1 (initiate) + number of 5 MB parts + 1 (complete). For example, a 12 MB file uses 4 PUT requests.
- Log chunks always use a single PUT request.

```text
s3_artifact_put_cost = artifact_put_requests * 0.000005 * (1 - upload_cost_discount)
s3_log_put_cost      = log_put_requests      * 0.000005 * (1 - upload_cost_discount)
```

## How is the S3 artifact storage cost calculated?

Storage cost is calculated using S3 Intelligent Tiering pricing, which transitions objects through three tiers based on how long they are retained in the bucket:

| Tier              | Days  | Price per GB-month |
| ----------------- | ----- | ------------------ |
| Standard          | 0–30  | $0.023             |
| Infrequent Access | 30–90 | $0.0125            |
| Archive           | 90+   | $0.004             |

The retention period is read from the bucket's S3 lifecycle rule. If no lifecycle rule is found for the bucket, the `default_max_artifact_expiration_days` value from the admin settings is used as a fallback. Separate discounts can be configured for each tier.

```text
days_in_standard = min(expiration_days, 30)
days_in_ia       = max(0, min(expiration_days, 90) - 30)
days_in_archive  = max(0, expiration_days - 90)

s3_artifact_storage_cost = upload_bytes
    * (days_in_standard * 0.023  * (1 - standard_storage_cost_discount) / GB / 30)
    + (days_in_ia       * 0.0125 * (1 - i_a_storage_cost_discount)      / GB / 30)
    + (days_in_archive  * 0.004  * (1 - archive_storage_cost_discount)   / GB / 30)
```

## How is the S3 log storage cost calculated?

S3 Log Storage Cost uses the same Intelligent Tiering pricing model and formula as S3 Artifact Storage Cost, applied to the log bytes uploaded and the log bucket's lifecycle rule retention period.

```text
s3_log_storage_cost = log_upload_bytes
    * (days_in_standard * 0.023  * (1 - standard_storage_cost_discount) / GB / 30)
    + (days_in_ia       * 0.0125 * (1 - i_a_storage_cost_discount)      / GB / 30)
    + (days_in_archive  * 0.004  * (1 - archive_storage_cost_discount)   / GB / 30)
```

## How can I view cost data via the REST API?

Cost fields are returned on the task and version endpoints.

**Task** — returns `task_cost`, `predicted_task_cost`, and `s3_usage`:

```text
GET https://evergreen.mongodb.com/rest/v2/tasks/{task_id}
```

**Version** — returns `cost` (aggregated actuals) and `predicted_cost` across all tasks in the version:

```text
GET https://evergreen.mongodb.com/rest/v2/versions/{version_id}
```

Both endpoints require authentication via `Api-User` and `Api-Key` headers.

## Why is a task's cost zero even though it ran?

A task's cost fields are only populated if the relevant admin configuration is present. If the `cost` admin settings have not been configured (discounts, formula, etc.), cost calculations return zero. EC2 and EBS costs also require the distro to have pricing data set.

Additionally, S3 PUT costs are only calculated if the task uploaded files. S3 artifact storage cost requires that the task's artifacts have a resolvable lifecycle rule or a configured `default_max_artifact_expiration_days` fallback.

## How are cost discounts configured?

Cost discounts are configured by Evergreen admins in the `cost` section of the admin settings. The following discounts are available:

- `upload_cost_discount`: Applied to all S3 PUT costs (artifacts and logs).
- `standard_storage_cost_discount`: Applied to S3 Standard tier storage costs.
- `i_a_storage_cost_discount`: Applied to S3 Infrequent Access tier storage costs.
- `archive_storage_cost_discount`: Applied to S3 Archive tier storage costs.
- `ebs_discount`: Applied to EBS GP3 throughput costs.
- `savings_plan_discount`: Applied to the savings plan component of EC2 adjusted cost.
- `on_demand_discount`: Applied to the on-demand component of EC2 on-demand and adjusted cost.

All discounts are values between 0.0 (no discount) and 1.0 (100% discount).
