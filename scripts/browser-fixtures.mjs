import fs from 'node:fs/promises';
import path from 'node:path';
import JSZip from 'jszip';

const root = path.resolve(import.meta.dirname,'..');
const zip = new JSZip();
const ignored = new Set(['node_modules','dist','uploads','output','.git','.env']);
async function collect(directory) {
  for (const entry of await fs.readdir(path.join(root,directory),{withFileTypes:true})) {
    if (ignored.has(entry.name)) continue;
    const relative = path.join(directory,entry.name);
    if (entry.isDirectory()) await collect(relative);
    else if (entry.isFile() && /\.(go|sql|ts|tsx|css|json|yaml|yml|html|md)$/.test(entry.name)) zip.file(relative,await fs.readFile(path.join(root,relative)),{createFolders:false});
  }
}
await collect('backend'); await collect('frontend');
zip.file('docker-compose.yml',await fs.readFile(path.join(root,'docker-compose.yml')),{createFolders:false});
zip.file('README.md',await fs.readFile(path.join(root,'README.md')),{createFolders:false});
const destination = path.join(root,'output/playwright');
await fs.mkdir(destination,{recursive:true});
await fs.writeFile(path.join(destination,'工程台-浏览器验收源码.zip'),await zip.generateAsync({type:'nodebuffer',compression:'DEFLATE'}));
console.log(path.join(destination,'工程台-浏览器验收源码.zip'));
