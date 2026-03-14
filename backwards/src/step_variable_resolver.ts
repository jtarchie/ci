/// <reference path="../../packages/pocketci/src/global.d.ts" />

export class StepVariableResolver {
  private jobParams: Record<string, string> = {};

  setJobParams(jobParams: Record<string, string>): void {
    this.jobParams = jobParams;
  }

  generateAcrossCombinations(
    acrossVars: AcrossVar[],
  ): Record<string, string>[] {
    if (acrossVars.length === 0) {
      return [{}];
    }

    const [first, ...rest] = acrossVars;
    const restCombinations = this.generateAcrossCombinations(rest);
    const combinations: Record<string, string>[] = [];

    for (const value of first.values) {
      for (const restCombination of restCombinations) {
        combinations.push({
          [first.var]: value,
          ...restCombination,
        });
      }
    }

    return combinations;
  }

  injectAcrossVariables(
    step: Step,
    variables: Record<string, string>,
  ): Step {
    const clonedStep = { ...step };

    if ("task" in clonedStep && clonedStep.config) {
      const varSuffix = Object.values(variables).join("-");
      (clonedStep as Record<string, unknown>).task = `${
        (clonedStep as Task).task
      }-${varSuffix}`;
      clonedStep.config = {
        ...clonedStep.config,
        env: {
          ...clonedStep.config.env,
          ...variables,
        },
      };
    }

    delete (clonedStep as Record<string, unknown>).across;
    delete (clonedStep as Record<string, unknown>).fail_fast;

    return clonedStep;
  }

  injectJobParams(step: Step): Step {
    if (Object.keys(this.jobParams).length === 0) {
      return step;
    }

    const clonedStep = { ...step };

    if ("task" in clonedStep && clonedStep.config) {
      clonedStep.config = {
        ...clonedStep.config,
        env: {
          ...this.jobParams,
          ...clonedStep.config.env,
        },
      };
    }

    return clonedStep;
  }
}
