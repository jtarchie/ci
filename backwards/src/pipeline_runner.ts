/// <reference path="../../packages/ci/src/global.d.ts" />

import { JobRunner } from "./job_runner.ts";

export class PipelineRunner {
  private jobResults: Map<string, boolean> = new Map();
  private executedJobs: string[] = [];

  constructor(private config: PipelineConfig) {
    this.validatePipelineConfig();
  }

  private validatePipelineConfig(): void {
    assert.truthy(
      this.config.jobs.length > 0,
      "Pipeline must have at least one job",
    );

    assert.truthy(
      this.config.jobs.every((job) => job.plan.length > 0),
      "Every job must have at least one step",
    );

    // Ensure job names are unique
    const jobNames = this.config.jobs.map((job) => job.name);
    assert.equal(
      jobNames.length,
      new Set(jobNames).size,
      "Job names must be unique",
    );

    // Validate that all passed constraints reference existing jobs
    if (this.config.jobs.length > 1) {
      this.validateJobDependencies();
    }

    if (this.config.resources.length > 0) {
      this.validateResources();
    }
  }

  private validateJobDependencies(): void {
    const jobNames = new Set(this.config.jobs.map((job) => job.name));

    // Check that all passed constraints reference existing jobs
    assert.truthy(
      this.config.jobs.every((job) =>
        job.plan.every((step) => {
          if ("get" in step && step.passed) {
            return step.passed.every((passedJob) => jobNames.has(passedJob));
          }
          return true;
        })
      ),
      "All passed constraints must reference existing jobs",
    );

    // Check for circular dependencies
    this.detectCircularDependencies();
  }

  private detectCircularDependencies(): void {
    // Build job dependency graph
    const graph: Record<string, string[]> = {};

    // Initialize empty adjacency lists
    for (const job of this.config.jobs) {
      graph[job.name] = [];
    }

    // Populate adjacency lists
    for (const job of this.config.jobs) {
      for (const step of job.plan) {
        if ("get" in step && step.passed) {
          for (const dependency of step.passed) {
            graph[dependency].push(job.name);
          }
        }
      }
    }

    // Check for cycles using DFS
    const visited = new Set<string>();
    const recStack = new Set<string>();

    const hasCycle = (node: string): boolean => {
      if (!visited.has(node)) {
        visited.add(node);
        recStack.add(node);

        for (const neighbor of graph[node]) {
          if (!visited.has(neighbor) && hasCycle(neighbor)) {
            return true;
          } else if (recStack.has(neighbor)) {
            return true;
          }
        }
      }

      recStack.delete(node);
      return false;
    };

    for (const job of this.config.jobs) {
      if (!visited.has(job.name) && hasCycle(job.name)) {
        assert.truthy(false, "Pipeline contains circular job dependencies");
      }
    }
  }

  private validateResources(): void {
    assert.truthy(
      this.config.resources.every((resource) =>
        this.config.resource_types.some((type) => type.name === resource.type)
      ),
      "Every resource must have a valid resource type",
    );

    assert.truthy(
      this.config.jobs.every((job) =>
        job.plan.every((step) => {
          if ("get" in step) {
            return this.config.resources.some((resource) =>
              resource.name === step.get
            );
          }
          return true; // not a resource step, ignore lookup
        })
      ),
      "Every get must have a resource reference",
    );
  }

  async run(): Promise<void> {
    // Find jobs with no dependencies
    const jobsWithNoDeps = this.findJobsWithNoDependencies();

    // Run jobs in dependency order
    for (const job of jobsWithNoDeps) {
      await this.runJob(job);
    }

    if (this.config.assert?.execution) {
      // this assures that the outputs are in the same order as the job
      assert.equal(this.executedJobs, this.config.assert.execution);
    }
  }

  private findJobsWithNoDependencies(): Job[] {
    return this.config.jobs.filter((job) => {
      return !job.plan.some((step) => {
        if ("get" in step && step.passed) {
          return true;
        }
        return false;
      });
    });
  }

  private async runJob(job: Job): Promise<void> {
    this.executedJobs.push(job.name);

    try {
      const jobRunner = new JobRunner(
        job,
        this.config.resources,
        this.config.resource_types,
      );
      await jobRunner.run();

      // Mark job as successful
      this.jobResults.set(job.name, true);

      // Find and run jobs that depend on this job
      await this.runDependentJobs(job.name);
    } catch (error) {
      // Mark job as failed
      this.jobResults.set(job.name, false);
      throw error;
    }
  }

  private async runDependentJobs(completedJobName: string): Promise<void> {
    const dependentJobs = this.findDependentJobs(completedJobName);

    for (const job of dependentJobs) {
      // Check if all dependencies are satisfied
      const canRun = this.canJobRun(job);
      if (canRun) {
        await this.runJob(job);
      }
    }
  }

  private findDependentJobs(jobName: string): Job[] {
    return this.config.jobs.filter((job) => {
      // Check if this job has a get step with a passed constraint including jobName
      return job.plan.some((step) => {
        if ("get" in step && step.passed && step.passed.includes(jobName)) {
          return true;
        }
        return false;
      });
    });
  }

  private canJobRun(job: Job): boolean {
    // Check if all passed constraints are satisfied
    for (const step of job.plan) {
      if ("get" in step && step.passed && step.passed.length > 0) {
        // Check if all jobs in passed constraint have completed successfully
        const allDependenciesMet = step.passed.every(
          (depJobName) => this.jobResults.get(depJobName) === true,
        );

        if (!allDependenciesMet) {
          return false;
        }
      }
    }

    return true;
  }
}
