import type { StrmTask } from "@/api/strm";

const ARCHIVE_WORD = "归档";

function normalized(value: string) {
  return value.trim().toLocaleLowerCase();
}

function trimArchiveSeparators(value: string) {
  return value.replace(/^[\s·•|/\\_\-:：]+|[\s·•|/\\_\-:：]+$/g, "");
}

// 媒体库与 STRM 任务的匹配规则：
// - 普通媒体库不读取归档任务；
// - 归档媒体库先按去掉“归档”后的名称寻找归档任务；
// - 如果存在对应的归档任务，只返回该类别的归档任务，避免“归档·剧集”混入“归档·电影”；
// - 没有对应归档任务时，再回退到同名的非归档任务。
export function matchBueiEmbyLibraryStrmTasks(tasks: StrmTask[], libraryName: string) {
  const name = normalized(libraryName);
  if (!name) return [] as StrmTask[];

  if (!name.includes(ARCHIVE_WORD)) {
    return tasks.filter((task) => {
      const taskName = normalized(task.name);
      return !taskName.includes(ARCHIVE_WORD) && taskName.includes(name);
    });
  }

  const baseName = trimArchiveSeparators(normalized(name.replaceAll(ARCHIVE_WORD, "")));
  if (!baseName) return [] as StrmTask[];
  const archiveTasks = tasks.filter((task) => {
    const taskName = normalized(task.name);
    return taskName.includes(ARCHIVE_WORD) && taskName.includes(baseName);
  });
  if (archiveTasks.length) return archiveTasks;
  return tasks.filter((task) => {
    const taskName = normalized(task.name);
    return !taskName.includes(ARCHIVE_WORD) && taskName.includes(baseName);
  });
}
