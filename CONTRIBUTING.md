# Contributing Guidelines

## Commit messages
Each PR should consist of a single commit with the following format:
```
Commit title no longer than 80 chars

Commit message can be as long as you want. It needs to be complete but minimal,
meaning it should contain all the information to understand the change but
exclude redundant information. Please keep the lines in the commit message up
to 80 chars as well.

[Jira/GH-issue number]
```

The reason for requesting to squash your PR into a single **logical** commit is because our merge policy will squash all the commits anyway before merging. When squashed by Github it will list all the commit titles and messages as bullets, therefore, it will be more informative if correctly written to begin with.

By using the `squash` merge policy on Github we ensure that each PR is treated as an "atomic" piece of code that can be easily monitored and reverted.

It is recommended to add the tracking Jira/GH-issue if such exist at the end of the commit message.
