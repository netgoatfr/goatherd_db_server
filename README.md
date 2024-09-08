# Goatherd's Storage solution !

> provide a seemless key-value storage service, with token authentification.
Values are stored as blobs if exceeding 1KB, but the size limit per token is 1GB.

You are limited to 10 requests per minutes using a standart token, but you can ask for this limit to be modified.

Don't try to guess the admin token, it's quite long and is reset each time the server restart.

You may also have an item number limit, or a max size.

## Enpoints: 
*  /&lt;db_name&gt;/ (Method:GET) if your token can access this database, will return a list of every keys in the database.
*  /&lt;db_name&gt;/&lt;key&gt; (Method:GET) if your token can access this database, will return the value of the key &quot;&lt;key&gt;&quot;
*  /&lt;db_name&gt;/&lt;key&gt; (Method:POST) if your token can access this database, and is NOT read-only, will set the content of the key &quot;&lt;key&gt;&quot; to the post request body.
*  /&lt;db_name&gt;/&lt;key&gt; (Method:DELETE) if your token can access this database, and is NOT read-only, will remove the key &quot;&lt;key&gt;&quot; from the database.

If an endpoint return an error, the error is described in the response body, and the status code is set accordingly.
Entirely made by [netgoatfr](https://discord.com/users/923609465637470218)
# HAVE A FANTASTIC DAY !