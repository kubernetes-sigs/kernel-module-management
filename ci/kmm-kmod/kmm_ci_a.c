/*
 *  kmm_ci.c - The simplest kernel module.
 */
#include <linux/init.h>
#include <linux/module.h>   /* Needed by all modules */
#include <linux/kernel.h>   /* Needed for KERN_INFO */
#include <linux/platform_device.h>
#include <linux/firmware.h>
#include <linux/fs.h>

MODULE_LICENSE("GPL");
MODULE_AUTHOR("Yevgeny Shnaidman");
MODULE_DESCRIPTION("A simple kernel module for KMM CI");
MODULE_VERSION("0.01");

static int __init kmm_ci_init(void)
{
    struct platform_device *pdev;
    const char fw_data[] = "kmm_ci_firmware validation string";
    const char fw_name[] = "kmm_ci_firmware.bin";
    const struct firmware *fw;
    int err;

    printk(KERN_INFO "Hello world from kmm_ci\n");

    pdev = platform_device_register_simple("kmm-ci-1", 0, NULL, 0);
    if (IS_ERR(pdev)) {
        printk(KERN_ERR "Failed to register device for \"%s\"\n", fw_name);
        err = -1;
        goto out;
    }

    err = request_firmware(&fw, fw_name, &pdev->dev);
    if (err) {
        printk(KERN_ERR "Failed to load image \"%s\" err %d\n", fw_name, err);
        goto unregister_platform;
    }

    if (fw->size != strlen(fw_data)) {
        printk(KERN_ERR "The firmware data size is different from what we expect: %ld != %ld\n", strlen(fw_data), fw->size);
        err = -1;
        goto release_firmware;
    }

    if (strncmp(fw_data, fw->data, fw->size)) {
        printk(KERN_ERR "The firmware data <%s> != the expected data <%s>\n", fw->data, fw_data);
        err = -1;
        goto release_firmware;
    }

    printk(KERN_INFO "ALL GOOD WITH FIRMWARE kmm_ci\n");

release_firmware:
    release_firmware(fw);
unregister_platform:
    platform_device_unregister(pdev);

    /*
     * A non 0 return means init_module failed; module can't be loaded.
     */
out:
    return err;
}

static void __exit kmm_ci_exit(void)
{
    printk(KERN_INFO "Goodbye world from kmm_ci.\n");
}

module_init(kmm_ci_init);
module_exit(kmm_ci_exit);
