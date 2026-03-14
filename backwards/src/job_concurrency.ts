import { TaskAbort, TaskErrored, TaskFailure } from "./task_runner.ts";

export interface ConcurrencyResult {
  failed: boolean;
  firstError?: unknown;
}

export class JobConcurrency {
  constructor(
    private jobMaxInFlight?: number,
    private pipelineMaxInFlight?: number,
  ) {}

  private getDefaultMaxInFlight(): number | undefined {
    if (this.jobMaxInFlight && this.jobMaxInFlight > 0) {
      return this.jobMaxInFlight;
    }

    if (this.pipelineMaxInFlight && this.pipelineMaxInFlight > 0) {
      return this.pipelineMaxInFlight;
    }

    return undefined;
  }

  private resolveMaxInFlight(localLimit?: number): number {
    const fallback = this.getDefaultMaxInFlight();
    if (fallback && fallback > 0) {
      return fallback;
    }

    if (localLimit && localLimit > 0) {
      return localLimit;
    }

    return Number.MAX_SAFE_INTEGER;
  }

  async runWithConcurrencyLimit<T>(
    items: T[],
    worker: (item: T, index: number) => Promise<void>,
    localLimit?: number,
    failFast: boolean = false,
  ): Promise<ConcurrencyResult> {
    if (items.length === 0) {
      return { failed: false };
    }

    const maxInFlight = Math.max(
      1,
      Math.min(this.resolveMaxInFlight(localLimit), items.length),
    );

    let nextIndex = 0;
    let activeCount = 0;
    let failed = false;
    const allErrors: unknown[] = [];

    await new Promise<void>((resolve) => {
      const launch = (): void => {
        if (nextIndex >= items.length && activeCount === 0) {
          resolve();
          return;
        }

        while (
          activeCount < maxInFlight &&
          nextIndex < items.length &&
          !(failFast && failed)
        ) {
          const currentIndex = nextIndex;
          nextIndex += 1;
          activeCount += 1;

          Promise.resolve(worker(items[currentIndex], currentIndex))
            .catch((error) => {
              failed = true;
              allErrors.push(error);
            })
            .finally(() => {
              activeCount -= 1;
              launch();
            });
        }

        if (
          (failFast && failed || nextIndex >= items.length) &&
          activeCount === 0
        ) {
          resolve();
        }
      };

      launch();
    });

    const firstError = allErrors.find((error) => error instanceof TaskAbort) ??
      allErrors.find((error) => error instanceof TaskErrored) ??
      allErrors.find((error) => error instanceof TaskFailure) ??
      allErrors[0];

    return { failed, firstError };
  }
}
