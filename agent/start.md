Our task is to write a go program that does three things:

1. Concurrently walk the current directory recursively to find all `.org` files, feed into memory safe common list
2. Concurrently, for each org file, mmaped as bytes:
   1. extract all the UUIDs (36 character format) from the property drawers (under the `:ID:` property) for all headings in each file by iterating linearly through the file and: 
      1. if `:ID: ` (space intentional) is hit
        1. starting a running list of bytes that we've come across,
        2. and if each subsequent byte matches exactly the 36 character, hex decimal, 8-4-4-4-12 format, adding that byte to the running list;
        3. but if that byte doesn't match the pattern, emptying the running list and starting over looking for a valid starting set of bytes (again, `:ID: `); 
      2. until we've collected all the UUIDs.
   2. Feed the file name and the IDs defined in it to a memory safe common mapping from "id:"+(the id) -> file name relative to base directory
3. Concurrently, for each org file, again mmapped as bytes:
   1. Use the already installed ahocorasick to replace any one of those UUIDs from the map keys in (2), if found, with the corresponding value of the key
   2. IF NOT `sitemap-preamble.org`: Then use the already installed go-org package to convert the org file to HTML
   3. IF NOT `sitemap-preamble.org`: Use html/template to stamp out a version of page-template.html with its contents subbed in for the html from the org mode file's conversion
4. Load in `sitemap-preamble.org`, extend it to contain: 
   1. org mode links to the html files corresponding to the five most recently modified org files, plus 500 character blurbs from the top of those files with special characters stripped
   2. org mode links to all of the html files corresponding to the `tag-*.org` files, with a list of the number of links in them
